// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"runtime"
	"strings"
	"sync"

	"golang.org/x/mod/semver"
	"golang.org/x/sync/errgroup"
	"gopkg.in/yaml.v3"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/data"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/ast"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

const (
	// matchDepsConcurrencyMultiplier controls the maximum number of concurrent goroutines
	// used in the matchDeps function. It multiplies the number of CPUs to determine
	// the concurrency limit for errgroup execution within matchDeps.
	matchDepsConcurrencyMultiplier = 2
)

// createRuleFromFields creates a rule instance based on the field type present in the YAML
//
//nolint:nilnil // factory function
func createRuleFromFields(raw []byte, name string, fields map[string]any) (rule.InstRule, error) {
	switch {
	case fields["struct"] != nil:
		return rule.NewInstStructRule(raw, name)
	case fields["file"] != nil:
		return rule.NewInstFileRule(raw, name)
	case fields["raw"] != nil:
		return rule.NewInstRawRule(raw, name)
	case fields["func"] != nil:
		return rule.NewInstFuncRule(raw, name)
	default:
		util.ShouldNotReachHere()
		return nil, nil
	}
}

// parseEmbeddedRule parses the embedded yaml rule file to concrete rule instances
func parseEmbeddedRule(path string) ([]rule.InstRule, error) {
	yamlFile, err := data.ReadEmbedFile(path)
	if err != nil {
		return nil, err
	}
	var h map[string]map[string]any
	err = yaml.Unmarshal(yamlFile, &h)
	if err != nil {
		return nil, ex.Wrap(err)
	}
	rules := make([]rule.InstRule, 0)
	for name, fields := range h {
		raw, err1 := yaml.Marshal(fields)
		if err1 != nil {
			return nil, ex.Wrap(err1)
		}

		r, err2 := createRuleFromFields(raw, name, fields)
		if err2 != nil {
			return nil, err2
		}
		rules = append(rules, r)
	}
	return rules, nil
}

// materializeRules materializes all available rules from the embedded data
func materializeRules() ([]rule.InstRule, error) {
	availables, err := data.ListEmbedFiles()
	if err != nil {
		return nil, err
	}

	parsedRules := []rule.InstRule{}
	for _, available := range availables {
		rs, perr := parseEmbeddedRule(available)
		if perr != nil {
			return nil, perr
		}
		parsedRules = append(parsedRules, rs...)
	}
	return parsedRules, nil
}

func matchVersion(dependency *Dependency, rule rule.InstRule) bool {
	// No version specified, so it's always applicable
	if rule.GetVersion() == "" {
		return true
	}

	// Version range? i.e. "v0.11.0,v0.12.0"
	ruleVersion := rule.GetVersion()
	if strings.Contains(ruleVersion, ",") {
		commaIndex := strings.Index(ruleVersion, ",")
		//nolint:gocritic // commaIndex is always valid
		startInclusive := ruleVersion[:commaIndex]
		endExclusive := ruleVersion[commaIndex+1:]
		// Version is in the "inclusive,exclusive" range
		if semver.Compare(dependency.Version, startInclusive) >= 0 &&
			semver.Compare(dependency.Version, endExclusive) < 0 {
			return true
		}
		return false
	}
	// Minimal version only? i.e. "v0.11.0"
	return semver.Compare(dependency.Version, ruleVersion) >= 0
}

// runMatch performs precise matching of rules against the dependency's source code.
// It parses source files and matches rules by examining AST nodes
func (sp *SetupPhase) runMatch(dep *Dependency, rulesByTarget map[string][]rule.InstRule) (*rule.InstRuleSet, error) {
	set := rule.NewInstRuleSet(dep.ImportPath)

	// Filter rules by target
	relevantRules := rulesByTarget[dep.ImportPath]
	if len(relevantRules) == 0 {
		return set, nil
	}

	// Filter rules by version
	filteredRules := make([]rule.InstRule, 0)
	for _, r := range relevantRules {
		if !matchVersion(dep, r) {
			continue
		}
		filteredRules = append(filteredRules, r)
	}

	// Separate file rules from rules that need precise matching
	preciseRules := make([]rule.InstRule, 0)
	for _, r := range filteredRules {
		// If the rule is a file rule, it is always applicable
		if fr, ok := r.(*rule.InstFileRule); ok {
			set.AddFileRule(fr)
			sp.Info("Match file rule", "rule", fr, "dep", dep)
			continue
		}
		// We can't decide whether the rule is applicable yet, add it to the
		// precise rules list to be processed later.
		preciseRules = append(preciseRules, r)
	}

	if len(preciseRules) == 0 {
		return set, nil
	}

	// Precise matching
	for _, source := range dep.Sources {
		// Parse the source code. Since the only purpose here is to match,
		// no node updates, we can use fast variant.
		tree, err := ast.ParseFileFast(source)
		if err != nil {
			return nil, err
		}
		if tree == nil {
			return nil, ex.Newf("failed to parse file %s", source)
		}
		set.SetPackageName(tree.Name.Name)

		for _, r := range preciseRules {
			// Let's match with the rule precisely
			switch rt := r.(type) {
			case *rule.InstFuncRule:
				funcDecl := ast.FindFuncDecl(tree, rt.Func, rt.Recv)
				if funcDecl != nil {
					set.AddFuncRule(source, rt)
					sp.Info("Match func rule", "rule", rt, "dep", dep)
				}
			case *rule.InstStructRule:
				structDecl := ast.FindStructDecl(tree, rt.Struct)
				if structDecl != nil {
					set.AddStructRule(source, rt)
					sp.Info("Match struct rule", "rule", rt, "dep", dep)
				}
			case *rule.InstRawRule:
				funcDecl := ast.FindFuncDecl(tree, rt.Func, rt.Recv)
				if funcDecl != nil {
					set.AddRawRule(source, rt)
					sp.Info("Match raw rule", "rule", rt, "dep", dep)
				}
			case *rule.InstFileRule:
				// Skip as it's already processed
				continue
			default:
				util.ShouldNotReachHere()
			}
		}
	}
	return set, nil
}

func (sp *SetupPhase) matchDeps(ctx context.Context, deps []*Dependency) ([]*rule.InstRuleSet, error) {
	// Construct the set of default allRules by parsing embedded data
	allRules, err := materializeRules()
	if err != nil {
		return nil, err
	}
	sp.Info("Found available rules", "rules", allRules)
	if len(allRules) == 0 {
		return nil, nil
	}

	// Pre-index rules by target
	rulesByTarget := make(map[string][]rule.InstRule)
	for _, r := range allRules {
		target := r.GetTarget()
		rulesByTarget[target] = append(rulesByTarget[target], r)
	}

	// Match the default rules with the found dependencies
	matched := make([]*rule.InstRuleSet, 0)
	var mu sync.Mutex
	g, _ := errgroup.WithContext(ctx)
	g.SetLimit(runtime.NumCPU() * matchDepsConcurrencyMultiplier)

	for _, dep := range deps {
		g.Go(func() error {
			m, err1 := sp.runMatch(dep, rulesByTarget)
			if err1 != nil {
				return err1
			}
			if !m.IsEmpty() {
				mu.Lock()
				matched = append(matched, m)
				mu.Unlock()
			}
			return nil
		})
	}

	if err = g.Wait(); err != nil {
		return nil, err
	}
	return matched, nil
}
