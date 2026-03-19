// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/dave/dst"
	"golang.org/x/mod/semver"
	"golang.org/x/sync/errgroup"
	"gopkg.in/yaml.v3"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/ast"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/filter"
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
	case fields["function_call"] != nil:
		return rule.NewInstCallRule(raw, name)
	default:
		util.ShouldNotReachHere()
		return nil, nil
	}
}

func parseRuleFromYaml(content []byte) ([]rule.InstRule, error) {
	var h map[string]map[string]any
	err := yaml.Unmarshal(content, &h)
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

func loadDefaultRules() ([]rule.InstRule, error) {
	// List all YAML files in the unzipped pkg directory, i.e. $BUILD_TEMP/pkg
	files, err := util.ListFiles(util.GetBuildTemp(unzippedPkgDir))
	if err != nil {
		return nil, err
	}
	// Parse all YAML contents into rule instances
	parsedRules := make([]rule.InstRule, 0)
	for _, file := range files {
		if !util.IsYamlFile(file) {
			continue
		}
		content, err1 := os.ReadFile(file)
		if err1 != nil {
			return nil, ex.Wrapf(err1, "failed to read YAML file %s", file)
		}
		rs, err2 := parseRuleFromYaml(content)
		if err2 != nil {
			return nil, err2
		}
		parsedRules = append(parsedRules, rs...)
	}
	return parsedRules, nil
}

func matchVersion(dependency *Dependency, rule rule.InstRule) bool {
	v := rule.GetVersion()
	// No version specified, so it's always applicable.
	if v == "" {
		return true
	}

	// Version range? i.e. "v0.11.0,v0.12.0" (inclusive start, exclusive end).
	if startInclusive, endExclusive, ok := strings.Cut(v, ","); ok {
		return semver.Compare(dependency.Version, startInclusive) >= 0 &&
			semver.Compare(dependency.Version, endExclusive) < 0
	}
	// Minimal version only? i.e. "v0.11.0"
	return semver.Compare(dependency.Version, v) >= 0
}

// runMatch performs precise matching of rules against the dependency's source code.
// It parses source files and matches rules by examining AST nodes.
//
// rules is a pre-merged slice containing exact-target rules for this dependency
// and any glob rules (those carrying an ImportPath filter) that must be evaluated
// against every dependency.
func (sp *SetupPhase) runMatch(
	ctx context.Context,
	dep *Dependency,
	rules []rule.InstRule,
) (*rule.InstRuleSet, error) {
	set := rule.NewInstRuleSet(dep.ImportPath)

	if len(dep.CgoFiles) > 0 {
		set.SetCgoFileMap(dep.CgoFiles)
		sp.Debug("Set CGO file map", "dep", dep.ImportPath, "cgoFiles", dep.CgoFiles)
	}

	if len(rules) == 0 {
		return set, nil
	}

	// Filter rules by version
	filteredRules := make([]rule.InstRule, 0, len(rules))
	for _, r := range rules {
		if !matchVersion(dep, r) {
			continue
		}
		filteredRules = append(filteredRules, r)
	}

	// Separate file rules from rules that need precise matching
	preciseRules := make([]rule.InstRule, 0, len(filteredRules))
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

	return sp.preciseMatching(ctx, dep, preciseRules, set)
}

// hasImportPathFilter reports whether r's Where clause contains an ImportPath
// predicate at any depth in the filter tree. Rules that have an ImportPath
// filter are routed through the globRules slice in matchDeps so they are
// evaluated against every dependency rather than only the exact target match.
func hasImportPathFilter(r rule.InstRule) bool {
	where := r.GetWhere()
	if where == nil {
		return false
	}
	return hasImportPathInDef(where)
}

// hasImportPathInDef recursively checks whether def or any of its children
// contains a non-empty ImportPath predicate.
func hasImportPathInDef(def *rule.FilterDef) bool {
	if def.ImportPath != "" {
		return true
	}
	for i := range def.AllOf {
		if hasImportPathInDef(&def.AllOf[i]) {
			return true
		}
	}
	for i := range def.OneOf {
		if hasImportPathInDef(&def.OneOf[i]) {
			return true
		}
	}
	if def.Not != nil {
		return hasImportPathInDef(def.Not)
	}
	return false
}

// ruleFilter pairs a rule with its pre-compiled Where filter (if any).
// Using a struct instead of parallel slices prevents index-desync bugs if
// the rules slice is ever sorted or deduplicated before this point.
type ruleFilter struct {
	rule   rule.InstRule
	filter filter.Filter // nil means no Where clause — apply unconditionally
}

// preciseMatching performs AST-based matching of instrumentation rules against
// the dependency's source files. It returns the rule set with the matched rules.
//
// If a rule carries a Where clause, the compiled Filter is evaluated against
// each source file before the standard AST match. Only files for which the
// filter passes proceed to the type-specific matching step.
func (sp *SetupPhase) preciseMatching(
	ctx context.Context,
	dep *Dependency,
	rules []rule.InstRule,
	set *rule.InstRuleSet,
) (*rule.InstRuleSet, error) {
	if len(dep.Sources) == 0 {
		return set, nil
	}

	// Pre-build filter trees for rules that carry a Where clause.
	// Filters are built once per rule and evaluated once per source file,
	// avoiding repeated construction inside the nested loops.
	ruleFilters := make([]ruleFilter, 0, len(rules))
	for _, r := range rules {
		var f filter.Filter
		if where := r.GetWhere(); where != nil {
			var err error
			f, err = filter.Build(where)
			if err != nil {
				return nil, ex.Wrapf(err, "build filter for rule %q", r.GetName())
			}
		}
		ruleFilters = append(ruleFilters, ruleFilter{rule: r, filter: f})
	}

	for _, source := range dep.Sources {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Parse the source code. Since the only purpose here is to match,
		// no node updates, we can use fast variant.
		tree, err := ast.ParseFileFast(source)
		if err != nil {
			return nil, err
		}
		// All files in a Go package share the same declared package name, so
		// this is idempotent across iterations; SetPackageName asserts non-empty.
		set.SetPackageName(tree.Name.Name)

		// mctx is allocated once per source file and reused across all rules
		// evaluated against that file. All fields are constant for a given
		// source file, so no updates are needed inside the inner loop.
		mctx := filter.MatchContext{
			ImportPath: dep.ImportPath,
			SourceFile: source,
			AST:        tree,
		}

		for _, rf := range ruleFilters {
			// Evaluate the Where filter if one is defined for this rule.
			// A nil filter means the rule applies to all files unconditionally.
			if rf.filter != nil && !rf.filter.Match(&mctx) {
				continue
			}
			sp.matchRule(rf.rule, source, tree, set, dep)
		}
	}
	return set, nil
}

// matchRule performs AST-based matching for a single rule against a single
// source file, adding the rule to set if it matches.
func (sp *SetupPhase) matchRule(
	r rule.InstRule,
	source string,
	tree *dst.File,
	set *rule.InstRuleSet,
	dep *Dependency,
) {
	// Each rule type uses a different AST query; dispatch to the correct handler.
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
	case *rule.InstCallRule:
		// Call rules are added unconditionally to all source files in the
		// target package. Unlike func/struct/raw rules, there is no cheap
		// AST predicate to pre-filter files (the matching requires import
		// alias resolution which happens during the instrument phase).
		// Files without matching calls are a no-op in applyCallRule.
		set.AddCallRule(source, rt)
		sp.Info("Match call rule", "rule", rt, "dep", dep)
	case *rule.InstFileRule:
		// Already dispatched in runMatch before preciseMatching is called.
	default:
		util.ShouldNotReachHere()
	}
}

func ruleFromDir(path string) ([]string, error) {
	ruleFilePatterns := []string{"*.otelc.yaml", "*.otelc.yml"}

	info, err := os.Stat(path)
	if err != nil {
		return nil, ex.Wrapf(err, "failed to stat %s", path)
	}

	if !info.IsDir() {
		return []string{path}, nil
	}

	var filesToProcess []string

	// Recursively traverse to each directories and include the rule files
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		var matched bool
		for _, pat := range ruleFilePatterns {
			matched, err = filepath.Match(pat, filepath.Base(p))
			if err != nil {
				return ex.Wrapf(err, "bad pattern %s", pat)
			}

			if matched {
				filesToProcess = append(filesToProcess, p)
				break
			}
		}

		return nil
	})
	if err != nil {
		return nil, ex.Wrapf(err, "failed to walk directory %s", path)
	}

	return filesToProcess, nil
}

func loadCustomRules(ruleConfig string) ([]rule.InstRule, error) {
	// Custom map to deduplicate rules
	ruleSet := make(map[string]rule.InstRule)
	ruleFiles := strings.SplitSeq(ruleConfig, ",")
	var content []byte
	for path := range ruleFiles {
		path = strings.TrimSpace(path)

		// Get all rule files from path (file or directory)
		files, err := ruleFromDir(path)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			content, err = os.ReadFile(file)
			if err != nil {
				return nil, ex.Wrapf(err, "failed to read %s from -rules flag", file)
			}

			var rules []rule.InstRule
			rules, err = parseRuleFromYaml(content)
			if err != nil {
				return nil, err
			}

			for _, r := range rules {
				ruleSet[r.GetName()] = r
			}
		}
	}

	return slices.Collect(maps.Values(ruleSet)), nil
}

func (sp *SetupPhase) loadRules() ([]rule.InstRule, error) {
	// Load rules from environment variable OTELC_RULES if specified. It has the
	// highest priority.
	rulePath := os.Getenv(util.EnvOtelcRules)
	if rulePath != "" {
		sp.Debug("rules source: environment variable %s (%s)", util.EnvOtelcRules, rulePath)
		content, err := os.ReadFile(filepath.Clean(rulePath))
		if err != nil {
			return nil, ex.Wrapf(err, "failed to read %s from env variable", rulePath)
		}
		return parseRuleFromYaml(content)
	}

	// Load custom rule(s) from config file if specified
	if sp.ruleConfig != "" {
		sp.Debug("rules source: ruleConfig (%s)", sp.ruleConfig)
		return loadCustomRules(sp.ruleConfig)
	}

	// Load default rules from the unzipped pkg directory
	sp.Debug("rules source: default rules")
	return loadDefaultRules()
}

func (sp *SetupPhase) matchDeps(ctx context.Context, deps []*Dependency) ([]*rule.InstRuleSet, error) {
	// Construct the set of default allRules by parsing embedded data
	allRules, err := sp.loadRules()
	if err != nil {
		return nil, err
	}
	sp.Info("Found available rules", "rules", allRules)
	if len(allRules) == 0 {
		return nil, nil
	}

	// Split rules into exact-target rules (fast map lookup) and glob rules
	// (evaluated against every dependency via their ImportPath filter).
	exactRules := make(map[string][]rule.InstRule)
	var globRules []rule.InstRule
	for _, r := range allRules {
		if hasImportPathFilter(r) {
			globRules = append(globRules, r)
		} else {
			target := r.GetTarget()
			exactRules[target] = append(exactRules[target], r)
		}
	}

	// Match the default rules with the found dependencies
	matched := make([]*rule.InstRuleSet, 0)
	var mu sync.Mutex
	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(runtime.NumCPU() * matchDepsConcurrencyMultiplier)

	for _, dep := range deps {
		g.Go(func() error {
			// Merge exact rules for this dep with glob rules that must be
			// evaluated against every dependency.
			rules := make([]rule.InstRule, 0, len(exactRules[dep.ImportPath])+len(globRules))
			rules = append(rules, exactRules[dep.ImportPath]...)
			rules = append(rules, globRules...)
			m, err1 := sp.runMatch(gCtx, dep, rules)
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
