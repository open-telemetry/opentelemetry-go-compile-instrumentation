// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"strings"

	"golang.org/x/mod/semver"
	"gopkg.in/yaml.v3"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/data"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/ast"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

// createRuleFromFields creates a rule instance based on the field type present in the YAML
//
//nolint:nilnil // factory function
func createRuleFromFields(raw []byte, name string, fields map[string]any) (rule.InstRule, error) {
	base := rule.InstBaseRule{
		Name: name,
	}
	if target, ok := fields["target"].(string); ok {
		base.Target = target
	}
	if fields["version"] != nil {
		v, ok := fields["version"].(string)
		util.Assert(ok, "version is not a string")
		base.Version = v
	}

	switch {
	case fields["struct"] != nil:
		var r rule.InstStructRule
		if err := yaml.Unmarshal(raw, &r); err != nil {
			return nil, ex.Wrap(err)
		}
		r.InstBaseRule = base
		return &r, nil
	case fields["file"] != nil:
		var r rule.InstFileRule
		if err := yaml.Unmarshal(raw, &r); err != nil {
			return nil, ex.Wrap(err)
		}
		r.InstBaseRule = base
		return &r, nil
	case fields["raw"] != nil:
		var r rule.InstRawRule
		if err := yaml.Unmarshal(raw, &r); err != nil {
			return nil, ex.Wrap(err)
		}
		r.InstBaseRule = base
		return &r, nil
	case fields["func"] != nil:
		var r rule.InstFuncRule
		if err := yaml.Unmarshal(raw, &r); err != nil {
			return nil, ex.Wrap(err)
		}
		r.InstBaseRule = base
		return &r, nil
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

// materalizeRules materializes all available rules from the embedded data
func materalizeRules() ([]rule.InstRule, error) {
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

func matchTarget(dependency *Dependency, rule rule.InstRule) bool {
	return rule.GetTarget() == dependency.ImportPath
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

func (sp *SetupPhase) runMatch(dependency *Dependency, allRules []rule.InstRule) (*rule.InstRuleSet, error) {
	set := rule.NewInstRuleSet(dependency.ImportPath)

	// Quick filtering
	availables := make([]rule.InstRule, 0)
	for _, r := range allRules {
		// Not all availables are applicable to the dependency, we can quickly
		// filter out the availables based on the target module path and the version.
		if !matchTarget(dependency, r) || !matchVersion(dependency, r) {
			continue
		}
		// If the rule is a file rule, it is always applicable
		if fr, ok := r.(*rule.InstFileRule); ok {
			set.AddFileRule(fr)
			sp.Info("Match file rule", "rule", fr, "dep", dependency)
			continue
		}
		// We can't decide whether the rule is applicable yet, add it to the
		// available list to be processed later.
		availables = append(availables, r)
	}
	// Precise matching
	for _, source := range dependency.Sources {
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

		for _, available := range availables {
			// Let's match with the rule precisely
			switch rt := available.(type) {
			case *rule.InstFuncRule:
				funcDecl := ast.FindFuncDecl(tree, rt.Func, rt.Recv)
				if funcDecl != nil {
					set.AddFuncRule(source, rt)
					sp.Info("Match func rule", "rule", rt, "dep", dependency)
				}
			case *rule.InstStructRule:
				structDecl := ast.FindStructDecl(tree, rt.Struct)
				if structDecl != nil {
					set.AddStructRule(source, rt)
					sp.Info("Match struct rule", "rule", rt, "dep", dependency)
				}
			case *rule.InstRawRule:
				funcDecl := ast.FindFuncDecl(tree, rt.Func, rt.Recv)
				if funcDecl != nil {
					set.AddRawRule(source, rt)
					sp.Info("Match raw rule", "rule", rt, "dep", dependency)
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

func (sp *SetupPhase) matchDeps(deps []*Dependency) ([]*rule.InstRuleSet, error) {
	// Construct the set of default allRules by parsing embedded data
	allRules, err := materalizeRules()
	if err != nil {
		return nil, err
	}
	sp.Info("Found available rules", "rules", allRules)
	if len(allRules) == 0 {
		return nil, nil
	}

	// Match the default rules with the found dependencies
	matched := make([]*rule.InstRuleSet, 0)
	for _, dep := range deps {
		// TODO: Parallelize this
		m, err1 := sp.runMatch(dep, allRules)
		if err1 != nil {
			return nil, err1
		}
		if !m.IsEmpty() {
			matched = append(matched, m)
		}
	}
	return matched, nil
}
