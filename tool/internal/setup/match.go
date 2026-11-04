// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/dave/dst"
	"golang.org/x/sync/errgroup"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

const (
	// matchDepsConcurrencyMultiplier controls the maximum number of concurrent goroutines
	// used in the matchDeps function. It multiplies the number of CPUs to determine
	// the concurrency limit for errgroup execution within matchDeps.
	matchDepsConcurrencyMultiplier = 2
)

func checkRuleFileVersion(ctx context.Context, path string, doc rule.File, current string) error {
	if doc.Legacy {
		util.LoggerFromContext(ctx).WarnContext(ctx,
			"rule file has no minimum otelc version; assuming legacy version", "file", path,
			"version", rule.LegacyVersion)
	}
	if err := rule.CheckVersion(current, doc.MinimumVersion); err != nil {
		return ex.Wrapf(err, "rule file %s", path)
	}
	return nil
}

// parseRuleFile parses file's already-read content into the rules it
// declares, checking the rule-file version along the way. Shared by
// loadCustomRuleFile and loadRulesFromToolFiles, which otherwise differ only
// in how they find file paths and in the wording each gives for its own
// read error. otelcVersion is the version of otelc that the rule file must
// support.
func parseRuleFile(ctx context.Context, file string, content []byte, otelcVersion string) ([]rule.InstRule, error) {
	doc, parseErr := rule.ParseFile(content)
	if parseErr != nil {
		return nil, ex.Wrapf(parseErr, "parsing rule file %s", file)
	}
	if err := checkRuleFileVersion(ctx, file, doc, otelcVersion); err != nil {
		return nil, err
	}
	return doc.Rules()
}

func matchVersion(dependency *Dependency, rule rule.InstRule) bool {
	return util.VersionInRange(dependency.Version, rule.GetVersion())
}

type targetRule struct {
	target *rule.Target
	rule   rule.InstRule
}

func (sp *setupPhase) matchGlobRules(
	dep *Dependency,
	relevantRules []rule.InstRule,
	globRules []targetRule,
) []rule.InstRule {
	var matched []rule.InstRule
	var seen map[rule.InstRule]bool
	for _, gr := range globRules {
		if !gr.target.Matches(dep.ImportPath, sp.rootModulePaths) {
			continue
		}
		if matched == nil {
			matched = make([]rule.InstRule, 0, len(relevantRules)+1)
			matched = append(matched, relevantRules...)
			seen = make(map[rule.InstRule]bool, len(relevantRules)+1)
			for _, r := range relevantRules {
				seen[r] = true
			}
		}
		if seen[gr.rule] {
			continue
		}
		seen[gr.rule] = true
		matched = append(matched, gr.rule)
		sp.Debug("Match target", "rule", gr.rule.GetName(), "target", gr.target.String(), "dep", dep.ImportPath)
	}
	if matched == nil {
		return relevantRules
	}
	return matched
}

// markIgnoredCallFiles records source files in dep that hold a
// //otelc:ignore comment directly above a call.
func markIgnoredCallFiles(dep *Dependency, set *rule.InstRuleSet) error {
	for _, source := range dep.Sources {
		maybe, err := ast.FileContainsDirectiveText(source, util.DirectiveIgnore)
		if err != nil {
			return ex.Wrapf(err, "scanning %s for %s", source, util.DirectiveIgnore)
		}
		if !maybe {
			continue
		}
		tree, err := ast.ParseFileFast(source)
		if err != nil {
			return ex.Wrapf(err, "parsing %s for %s", source, util.DirectiveIgnore)
		}
		if ast.FileHasIgnoredCall(tree, util.DirectiveIgnore) {
			set.AddIgnoredCallFile(source)
		}
	}
	return nil
}

// runMatch performs precise matching of rules against the dependency's source code.
// It parses source files and matches rules by examining AST nodes.
//
// Rules reach this function through two paths:
//   - exactRules is the rule index keyed by exact target import path. The fast
//     path is a single map lookup on dep.ImportPath.
//   - globRules are rules whose target is anything but a single exact import
//     path (a glob, $root, or a list); each one's target is evaluated against
//     dep.ImportPath because it cannot be pre-indexed by key.
func (sp *setupPhase) runMatch(
	ctx context.Context,
	dep *Dependency,
	exactRules map[string][]rule.InstRule,
	globRules []targetRule,
) (*rule.InstRuleSet, error) {
	set := rule.NewInstRuleSet(dep.ImportPath)
	set.IsTest = dep.IsTest

	if len(dep.CgoFiles) > 0 {
		set.SetCgoFileMap(dep.CgoFiles)
		sp.Debug("Set CGO file map", "dep", dep.ImportPath, "cgoFiles", dep.CgoFiles)
	}

	if err := markIgnoredCallFiles(dep, set); err != nil {
		return nil, err
	}

	// Fast path: exact-target rules via a single map lookup.
	relevantRules := exactRules[dep.ImportPath]

	// Glob path: a rule applies when its glob target matches this dependency's
	// import path. The combined slice is built lazily, on the first glob match,
	// so a dependency that matches no glob rule (the common case) stays
	// allocation-free. When built, it is a fresh slice to avoid aliasing the
	// exact-match slice shared read-only across goroutines.
	if len(globRules) > 0 {
		relevantRules = sp.matchGlobRules(dep, relevantRules, globRules)
	}

	if len(relevantRules) == 0 {
		return set, nil
	}

	// filter rules by version
	filteredRules := make([]rule.InstRule, 0, len(relevantRules))
	for _, r := range relevantRules {
		if !matchVersion(dep, r) {
			if unresolvedVersionSkip(dep.Version, r.GetVersion()) {
				// Per-rule: setup drops individual rules, so each skip is actionable.
				warnUnresolvedVersionSkip(sp.Warn, dep.ImportPath, r.GetVersion(),
					"rule", r.GetName(),
				)
			}
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
		if !set.IsEmpty() && len(dep.Sources) > 0 {
			name, err := ast.ParsePackageName(dep.Sources[0])
			if err != nil {
				return nil, err
			}
			set.SetPackageName(name)
		}

		return set, nil
	}

	return sp.preciseMatching(ctx, dep, preciseRules, set)
}

// ruleFilter pairs a rule with its pre-compiled where filter (if any).
// Using a struct instead of parallel slices prevents index-desync bugs if
// the rules slice is ever sorted or deduplicated before this point.
type ruleFilter struct {
	rule  rule.InstRule
	where filter // nil means no where clause — apply unconditionally
}

// preciseMatching performs AST-based matching of instrumentation rules against
// the dependency's source files. It returns the rule set with the matched rules.
//
// If a rule carries a where clause, the compiled filter is evaluated against
// each source file before the standard AST match. Only files for which the
// filter passes proceed to the type-specific matching step.
func (sp *setupPhase) preciseMatching(
	ctx context.Context,
	dep *Dependency,
	rules []rule.InstRule,
	set *rule.InstRuleSet,
) (*rule.InstRuleSet, error) {
	if len(dep.Sources) == 0 {
		return set, nil
	}

	// Pre-build filter trees for rules that carry a where clause.
	// Filters are compiled once per rule before source-file iteration, not
	// once per source file. In practice each rule targets exactly one import
	// path, so each filter is built once across the entire matchDeps run.
	ruleFilters := make([]ruleFilter, 0, len(rules))
	for _, r := range rules {
		var f filter
		if where := r.GetWhere(); where != nil {
			var err error
			f, err = build(where)
			if err != nil {
				return nil, ex.Wrapf(err, "build where filter for rule %q", r.GetName())
			}
		}
		ruleFilters = append(ruleFilters, ruleFilter{rule: r, where: f})
	}

	// IsTest is a property of the whole compile (every file in a test build
	// shares it), so compute it once and reuse it across each file's context.
	isTest := util.IsTestBuild(dep.Sources)

	known := knownDirectiveNames(rules)

	for _, source := range dep.Sources {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Parse the source code. Since the only purpose here is to match,
		// no node updates, we can use the fast variant.
		//
		// Contract: ParseFileFast returns (non-nil, nil) on success and
		// (nil, non-nil error) on failure. A (nil, nil) return is not
		// possible per the Go stdlib parser.ParseFile and dave/dst
		// DecorateFile contracts that ParseFileFast composes.
		tree, err := ast.ParseFileFast(source)
		if err != nil {
			return nil, err
		}
		// All files in a Go package share the same declared package name, so
		// this is idempotent across iterations; SetPackageName asserts non-empty.
		set.SetPackageName(tree.Name.Name)

		sp.warnUnknownDirectives(source, tree, known)

		// mctx is allocated once per source file and reused across all rules
		// evaluated against that file. All fields are constant for a given
		// source file, so no updates are needed inside the inner loop.
		mctx := matchContext{
			IsTest:        isTest,
			SourceFile:    source,
			AST:           tree,
			ResolvedNames: sp.resolvedNames,
		}

		for _, rf := range ruleFilters {
			// Evaluate the where filter if one is defined for this rule.
			// A nil filter means the rule applies to all files unconditionally.
			if rf.where != nil && !rf.where.Match(&mctx) {
				continue
			}
			if err = sp.matchOneRule(tree, source, rf.rule, set, dep); err != nil {
				return nil, err
			}
		}
	}
	return set, nil
}

func knownDirectiveNames(rules []rule.InstRule) map[string]bool {
	known := make(map[string]bool, len(util.BuiltinDirectives))
	for _, name := range util.BuiltinDirectives {
		known[name] = true
	}
	for _, r := range rules {
		if dr, ok := r.(*rule.InstDirectiveRule); ok {
			known[dr.Directive] = true
		}
	}
	return known
}

// warnUnknownDirectives logs a warning for every otelc: directive comment in
// tree that matches no name in known.
func (sp *setupPhase) warnUnknownDirectives(source string, tree *dst.File, known map[string]bool) {
	for _, name := range ast.UnknownDirectiveNames(tree, known) {
		sp.Warn("Unrecognized directive comment", "directive", name, "file", source)
	}
}

// matchOneRule performs precise AST matching for a single rule against a parsed
// source file, adding the rule to the set if it matches.
func (sp *setupPhase) matchOneRule(
	tree *dst.File,
	source string,
	r rule.InstRule,
	set *rule.InstRuleSet,
	dep *Dependency,
) error {
	switch rt := r.(type) {
	case *rule.InstFuncRule:
		_, ok, err := ast.FindFuncDecl(tree, rt, sp.resolvedNames)
		if err != nil {
			return err
		}
		if ok {
			set.AddFuncRule(source, rt)
			sp.Info("Match func rule", "rule", rt, "dep", dep)
		}
	case *rule.InstStructRule:
		structType := ast.FindStructType(tree, rt.Struct)
		if structType != nil {
			set.AddStructRule(source, rt)
			sp.Info("Match struct rule", "rule", rt, "dep", dep)
		}
	case *rule.InstRawRule:
		_, ok, err := ast.FindFuncDecl(tree, rt, sp.resolvedNames)
		if err != nil {
			return err
		}
		if ok {
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
	case *rule.InstLitRule:
		// Added unconditionally for the same reason as call rules above:
		// resolving the literal's type needs the file's import aliases.
		set.AddLitRule(source, rt)
		sp.Info("Match literal rule", "rule", rt, "dep", dep)
	case *rule.InstDirectiveRule:
		if ast.FileHasDirective(tree, rt.Directive) {
			set.AddDirectiveRule(source, rt)
			sp.Info("Match directive rule", "rule", rt, "dep", dep)
		}
	case *rule.InstDeclRule:
		if ast.FindNamedDecl(tree, rt.Identifier, rt.Kind) != nil {
			set.AddDeclRule(source, rt)
			sp.Info("Match decl rule", "rule", rt, "dep", dep)
		}
	case *rule.InstFileRule:
		// Skip as it's already processed
	default:
		util.ShouldNotReachHere()
	}
	return nil
}

func rulesFromDir(path string, skipSubmodules bool) ([]string, error) {
	var filesToProcess []string

	// Recursively traverse to each directories and include the rule files
	err := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return ex.Wrap(err)
		}

		if skipSubmodules && d.IsDir() && p != path && util.PathExists(filepath.Join(p, goModFileName)) {
			return filepath.SkipDir
		}

		if !d.IsDir() && util.IsRuleFile(d.Name()) {
			filesToProcess = append(filesToProcess, p)
		}

		return nil
	})
	if err != nil {
		return nil, ex.Wrapf(err, "failed to walk directory %s", path)
	}

	return filesToProcess, nil
}

// customRuleFiles returns the rule files at path. path is a rule file or a
// directory that contains rule files.
func customRuleFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, ex.Wrapf(err, "failed to stat %s", path)
	}
	if info.IsDir() {
		return rulesFromDir(path, false)
	}
	return []string{path}, nil
}

// loadCustomRuleFile reads and parses the rule file at file. It returns the
// rules of the file, grouped by entry name. otelcVersion is the version of
// otelc that the rule file must support.
func loadCustomRuleFile(
	ctx context.Context,
	file, otelcVersion string,
) (map[string][]rule.InstRule, error) {
	content, err := os.ReadFile(file)
	if err != nil {
		return nil, ex.Wrapf(err, "failed to read %s from -rules flag", file)
	}
	rules, err := parseRuleFile(ctx, file, content, otelcVersion)
	if err != nil {
		return nil, err
	}

	grouped := make(map[string][]rule.InstRule)
	for _, r := range rules {
		grouped[r.GetName()] = append(grouped[r.GetName()], r)
	}
	return grouped, nil
}

func loadCustomRules(ctx context.Context, ruleConfig, otelcVersion string) ([]rule.InstRule, error) {
	// Deduplicate by YAML-entry name. A single entry can expand into several
	// rules (e.g. a do: sequence with multiple modifiers), all sharing that
	// name, so each name maps to the full slice of rules it produced. When two
	// files define the same name, the last file in the list wins, and its
	// group replaces the earlier group as a unit.
	ruleSet := make(map[string][]rule.InstRule)
	// A file can be in the list more than one time, directly or through a
	// directory. Load each rule file only one time, and keep its rules. Each
	// time the file is in the list again, apply its rules again, so that the
	// result is the same as when the file is loaded again.
	loadedFiles := make(map[string]map[string][]rule.InstRule)
	for path := range strings.SplitSeq(ruleConfig, ",") {
		files, err := customRuleFiles(strings.TrimSpace(path))
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			key := ruleFileKey(file)
			grouped, loaded := loadedFiles[key]
			if !loaded {
				grouped, err = loadCustomRuleFile(ctx, file, otelcVersion)
				if err != nil {
					return nil, err
				}
				loadedFiles[key] = grouped
			}
			maps.Copy(ruleSet, grouped)
		}
	}

	total := 0
	for _, rules := range ruleSet {
		total += len(rules)
	}
	result := make([]rule.InstRule, 0, total)
	for _, name := range slices.Sorted(maps.Keys(ruleSet)) {
		result = append(result, ruleSet[name]...)
	}
	return result, nil
}

func loadRulesFromToolFiles(
	ctx context.Context,
	toolFiles []string,
	otelcVersion string,
) ([]rule.InstRule, error) {
	ruleSet := make([]rule.InstRule, 0)
	// Two imports can find the same rule file. For example, the walk of the
	// directory of "a/b" also finds the rule files of the sub-package "a/b/c".
	// Load each rule file only one time.
	seenRuleFiles := make(map[string]bool)
	walkErr := walkInstrumentation(ctx, toolFiles, func(v *instrumentationVisit) (bool, error) {
		if v.Config.Error != nil {
			return false, v.Config.Error
		}

		for _, file := range v.Config.RuleFiles {
			key := ruleFileKey(file)
			if seenRuleFiles[key] {
				continue
			}
			seenRuleFiles[key] = true

			content, readErr := os.ReadFile(file)
			if readErr != nil {
				return false, ex.Wrapf(readErr, "reading %s", file)
			}
			rules, parseErr := parseRuleFile(ctx, file, content, otelcVersion)
			if parseErr != nil {
				return false, parseErr
			}

			ruleSet = append(ruleSet, rules...)
		}
		return true, nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	return ruleSet, nil
}

// ruleFileKey returns a key that identifies the rule file at path. Different
// paths to the same file (relative paths, symbolic links) give the same key.
// If the path cannot be resolved, the key is the cleaned path.
func ruleFileKey(path string) string {
	// Resolve symbolic links first, on the raw (possibly relative) path:
	// filepath.Abs cleans ".." components lexically, so running it before
	// EvalSymlinks would delete a ".." path segment's preceding component
	// without ever resolving the symbolic link it might have named, letting
	// two different files collide on the same key. EvalSymlinks still
	// resolves correctly on a relative path, and filepath.Abs below anchors
	// the result, covering a working directory that itself contains a
	// symbolic link.
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	if abs, absErr := filepath.Abs(path); absErr == nil {
		// Abs joins a relative path using the working directory as Getwd
		// reports it, which on some platforms doesn't itself resolve every
		// symbolic link (macOS's /var -> /private/var APFS firmlink is one:
		// Getwd keeps the firmlinked form, but an explicit EvalSymlinks call
		// on the resulting absolute path still sees through it). Resolve
		// again on the now-absolute result to catch that.
		if resolved, evalErr := filepath.EvalSymlinks(abs); evalErr == nil {
			return resolved
		}
		return abs
	}
	return filepath.Clean(path)
}

func (sp *setupPhase) loadRules(ctx context.Context, moduleDirs []string) ([]rule.InstRule, error) {
	// Load rules from environment variable OTELC_RULES if specified. It has the
	// highest priority.
	rulePath := os.Getenv(util.EnvOtelcRules)
	if rulePath != "" {
		sp.Debug("rules source: environment variable %s (%s)", util.EnvOtelcRules, rulePath)
		return loadCustomRules(ctx, rulePath, util.Version)
	}

	// Load custom rule(s) from config file if specified
	if sp.ruleConfig != "" {
		sp.Debug("rules source: ruleConfig (%s)", sp.ruleConfig)
		return loadCustomRules(ctx, sp.ruleConfig, util.Version)
	}

	// Load rules from tool files if available
	toolFiles, err := findToolFiles(moduleDirs)
	if err != nil {
		return nil, err
	}
	if len(toolFiles) > 0 {
		sp.Debug("rules source: tool files", "toolFiles", toolFiles)
		return loadRulesFromToolFiles(ctx, toolFiles, util.Version)
	}

	// No tool files generated? Currently this part is un-reachable because
	// auto-pinning always generates tool files.
	return nil, nil
}

func (sp *setupPhase) matchDeps(
	ctx context.Context,
	deps []*Dependency,
	moduleDirs []string,
) ([]*rule.InstRuleSet, error) {
	// Construct the set of default allRules by parsing embedded data
	allRules, err := sp.loadRules(ctx, moduleDirs)
	if err != nil {
		return nil, err
	}
	sp.Info("Found available rules", "rules", allRules)
	if len(allRules) == 0 {
		return nil, nil
	}

	// Split rules into two matching tiers. Rules whose target is a single exact
	// import path are pre-indexed so each dependency resolves them with one map
	// lookup (unchanged fast path). Every other target (a glob, $root, or a
	// list) cannot be keyed, so those rules are kept in a flat slice and
	// evaluated against every dependency's import path.
	exactRules := make(map[string][]rule.InstRule)
	globRules := make([]targetRule, 0)
	for _, r := range allRules {
		target := r.GetTarget()
		if path, ok := target.Exact(); ok {
			exactRules[path] = append(exactRules[path], r)
			continue
		}
		if target.UsesRoot() {
			if len(sp.rootModulePaths) == 0 && len(sp.buildPackages) > 0 {
				sp.rootModulePaths, err = rootModulePaths(ctx, sp.buildPackages)
				if err != nil {
					return nil, err
				}
			}
			if len(sp.rootModulePaths) == 0 {
				return nil, ex.Newf("rule %q uses target %q, but no root module was found",
					r.GetName(), rule.TargetRoot)
			}
		}
		globRules = append(globRules, targetRule{target: target, rule: r})
	}

	// Match the default rules with the found dependencies
	matched := make([]*rule.InstRuleSet, 0)
	var mu sync.Mutex
	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(runtime.NumCPU() * matchDepsConcurrencyMultiplier)

	for _, dep := range deps {
		g.Go(func() error {
			m, err1 := sp.runMatch(gCtx, dep, exactRules, globRules)
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
	err = sortMatchedRuleSets(matched)
	if err != nil {
		return nil, err
	}
	return matched, nil
}

// sortMatchedRuleSets gives the matched-rule file a stable order. Matching is
// concurrent, so the order in which rule sets finish is not part of the build
// input. ModulePath is the primary key; the encoded rule set breaks ties for
// separate source variants of the same package (for example, test builds).
func sortMatchedRuleSets(matched []*rule.InstRuleSet) error {
	type keyedRuleSet struct {
		set *rule.InstRuleSet
		key []byte
	}

	keyed := make([]keyedRuleSet, len(matched))
	for i, set := range matched {
		key, err := json.Marshal(set)
		if err != nil {
			return ex.Wrapf(err, "failed to marshal matched rules for sorting")
		}
		keyed[i] = keyedRuleSet{set: set, key: key}
	}

	slices.SortStableFunc(keyed, func(a, b keyedRuleSet) int {
		if cmp := strings.Compare(a.set.ModulePath, b.set.ModulePath); cmp != 0 {
			return cmp
		}
		return bytes.Compare(a.key, b.key)
	})
	for i, item := range keyed {
		matched[i] = item.set
	}
	return nil
}
