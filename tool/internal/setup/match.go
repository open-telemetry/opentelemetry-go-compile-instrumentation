// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"
	"gopkg.in/yaml.v3"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/match"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

const (
	// matchDepsConcurrencyMultiplier controls the maximum number of concurrent goroutines
	// used in the matchDeps function. It multiplies the number of CPUs to determine
	// the concurrency limit for errgroup execution within matchDeps.
	matchDepsConcurrencyMultiplier = 2
)

// createRuleFromFields creates a rule instance based on the field type present
// in the (already-normalized) flat YAML fields map produced by [rule.Normalize].
func createRuleFromFields(raw []byte, name string, fields map[string]any) (rule.InstRule, error) {
	switch {
	case fields[rule.SelStruct] != nil:
		return rule.NewInstStructRule(raw, name)
	case fields[rule.WhereFile] != nil:
		return rule.NewInstFileRule(raw, name)
	case fields[rule.SelDirective] != nil:
		return rule.NewInstDirectiveRule(raw, name)
	case fields[rule.RawField] != nil:
		return rule.NewInstRawRule(raw, name)
	case fields[rule.SelFunc] != nil:
		return rule.NewInstFuncRule(raw, name)
	case fields[rule.SelFunctionCall] != nil:
		return rule.NewInstCallRule(raw, name)
	case fields[rule.SelIdentifier] != nil:
		return rule.NewInstDeclRule(raw, name)
	default:
		return nil, ex.Newf("rule %q has no recognised selector", name)
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
		flatRules, normErr := rule.Normalize(fields)
		if normErr != nil {
			return nil, normErr
		}
		for _, flatFields := range flatRules {
			raw, err1 := yaml.Marshal(flatFields)
			if err1 != nil {
				return nil, ex.Wrap(err1)
			}

			r, err2 := createRuleFromFields(raw, name, flatFields)
			if err2 != nil {
				return nil, err2
			}
			// target is the sole package selector and is required (docs/rules.md).
			// An empty or whitespace-only target would land under exactRules[""]
			// and silently never match any real import path, so reject it loudly
			// at load time instead.
			if strings.TrimSpace(r.GetTarget()) == "" {
				return nil, ex.Newf("rule %q has an empty target; target is required", name)
			}
			// Reject ambiguous/invalid glob targets at load time so a bad rule
			// fails loudly during parsing rather than silently matching nothing
			// during the setup phase.
			if err3 := rule.ValidateTarget(r.GetTarget()); err3 != nil {
				return nil, ex.Wrapf(err3, "rule %q", name)
			}
			if err3 := util.ValidateVersionRange(r.GetVersion()); err3 != nil {
				return nil, ex.Wrapf(err3, "rule %q", name)
			}
			rules = append(rules, r)
		}
	}
	return rules, nil
}

// isRuleFile checks if the given file name matches the following patterns:
// otelc.yml, otelc.yaml, *.otelc.yml, *.otelc.yaml
func isRuleFile(name string) bool {
	return (name == "otelc.yml" ||
		name == "otelc.yaml" ||
		strings.HasSuffix(name, ".otelc.yml") ||
		strings.HasSuffix(name, ".otelc.yaml"))
}

func matchVersion(dependency *Dependency, rule rule.InstRule) bool {
	return util.VersionInRange(dependency.Version, rule.GetVersion())
}

type targetRule struct {
	target string
	rule   rule.InstRule
}

func (sp *SetupPhase) matchGlobRules(
	dep *Dependency,
	relevantRules []rule.InstRule,
	globRules []targetRule,
) []rule.InstRule {
	var matched []rule.InstRule
	var seen map[rule.InstRule]bool
	for _, gr := range globRules {
		if !rule.MatchGlobTarget(gr.target, dep.ImportPath) {
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
		sp.Debug("Match glob target", "rule", gr.rule.GetName(), "target", gr.target, "dep", dep.ImportPath)
	}
	if matched == nil {
		return relevantRules
	}
	return matched
}

// runMatch performs precise matching of rules against the dependency's source code.
// It parses source files and matches rules by examining AST nodes.
//
// Rules reach this function through two paths:
//   - exactRules is the rule index keyed by exact target import path. The fast
//     path is a single map lookup on dep.ImportPath.
//   - globRules are rules whose target uses glob syntax; each one's pattern is
//     evaluated against dep.ImportPath because they cannot be pre-indexed by key.
func (sp *SetupPhase) runMatch(
	ctx context.Context,
	dep *Dependency,
	exactRules map[string][]rule.InstRule,
	globRules []targetRule,
) (*rule.InstRuleSet, error) {
	set := rule.NewInstRuleSet(dep.ImportPath)
	set.Version = dep.Version

	if len(dep.CgoFiles) > 0 {
		set.SetCgoFileMap(dep.CgoFiles)
		sp.Debug("Set CGO file map", "dep", dep.ImportPath, "cgoFiles", dep.CgoFiles)
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

	// Filter rules by version
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
	// Package-level candidates: target+version only. File-keyed maps below
	// remain the setup-time AST match that toolexec consumes today.
	set.SetCandidates(filteredRules)

	err := match.Apply(ctx, match.Input{
		Set:     set,
		Sources: dep.Sources,
		Rules:   filteredRules,
		Log:     sp,
		Dep:     dep,
	})
	if err != nil {
		return nil, err
	}
	return set, nil
}

// preciseMatching is a test helper that runs file/AST matching without the
// target+version filter. Production matching goes through runMatch.
func (sp *SetupPhase) preciseMatching(
	ctx context.Context,
	dep *Dependency,
	rules []rule.InstRule,
	set *rule.InstRuleSet,
) (*rule.InstRuleSet, error) {
	err := match.Apply(ctx, match.Input{
		Set:     set,
		Sources: dep.Sources,
		Rules:   rules,
		Log:     sp,
		Dep:     dep,
	})
	if err != nil {
		return nil, err
	}
	return set, nil
}

func rulesFromDir(path string, skipSubmodules bool) ([]string, error) {
	var filesToProcess []string

	// Recursively traverse to each directories and include the rule files
	err := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if skipSubmodules && d.IsDir() && p != path && util.PathExists(filepath.Join(p, "go.mod")) {
			return filepath.SkipDir
		}

		if !d.IsDir() && isRuleFile(d.Name()) {
			filesToProcess = append(filesToProcess, p)
		}

		return nil
	})
	if err != nil {
		return nil, ex.Wrapf(err, "failed to walk directory %s", path)
	}

	return filesToProcess, nil
}

func loadCustomRules(ruleConfig string) ([]rule.InstRule, error) {
	// Deduplicate by YAML-entry name. A single entry can expand into several
	// rules (e.g. a do: sequence with multiple modifiers), all sharing that
	// name, so each name maps to the full slice of rules it produced. Re-reading
	// the same entry replaces the whole group as a unit, preserving the
	// "same rule file passed twice should dedupe" behavior.
	ruleSet := make(map[string][]rule.InstRule)
	ruleFiles := strings.SplitSeq(ruleConfig, ",")
	var content []byte
	for path := range ruleFiles {
		path = strings.TrimSpace(path)

		// Get all rule files from path (file or directory)
		info, err := os.Stat(path)
		if err != nil {
			return nil, ex.Wrapf(err, "failed to stat %s", path)
		}

		var files []string
		if info.IsDir() {
			files, err = rulesFromDir(path, false)
			if err != nil {
				return nil, err
			}
		} else {
			files = []string{path}
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

			// Group this file's rules by entry name, then replace any
			// previously-seen entry of the same name as a single unit.
			grouped := make(map[string][]rule.InstRule)
			for _, r := range rules {
				grouped[r.GetName()] = append(grouped[r.GetName()], r)
			}
			maps.Copy(ruleSet, grouped)
		}
	}

	return slices.Concat(slices.Collect(maps.Values(ruleSet))...), nil
}

func loadRulesFromToolFiles(ctx context.Context, toolFiles []string) ([]rule.InstRule, error) {
	ruleSet := make([]rule.InstRule, 0)
	walkErr := walkInstrumentation(ctx, toolFiles, func(v *InstrumentationVisit) (bool, error) {
		if v.Config.Error != nil {
			return false, v.Config.Error
		}

		for _, file := range v.Config.RuleFiles {
			content, readErr := os.ReadFile(file)
			if readErr != nil {
				return false, ex.Wrapf(readErr, "reading %s", file)
			}

			rules, parseErr := parseRuleFromYaml(content)
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

func (sp *SetupPhase) loadRules(ctx context.Context, moduleDirs map[string]bool) ([]rule.InstRule, error) {
	// Load rules from environment variable OTELC_RULES if specified. It has the
	// highest priority.
	rulePath := os.Getenv(util.EnvOtelcRules)
	if rulePath != "" {
		sp.Debug("rules source: environment variable %s (%s)", util.EnvOtelcRules, rulePath)
		return loadCustomRules(rulePath)
	}

	// Load custom rule(s) from config file if specified
	if sp.ruleConfig != "" {
		sp.Debug("rules source: ruleConfig (%s)", sp.ruleConfig)
		return loadCustomRules(sp.ruleConfig)
	}

	// Load rules from tool files if available
	toolFiles, err := findToolFiles(moduleDirs)
	if err != nil {
		return nil, err
	}
	if len(toolFiles) > 0 {
		sp.Debug("rules source: tool files", "toolFiles", toolFiles)
		return loadRulesFromToolFiles(ctx, toolFiles)
	}

	// No tool files generated? Currently this part is un-reachable because
	// auto-pinning always generates tool files.
	return nil, nil
}

func (sp *SetupPhase) matchDeps(
	ctx context.Context,
	deps []*Dependency,
	moduleDirs map[string]bool,
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

	// Split rules into two matching tiers. Exact-target rules are pre-indexed
	// by import path so each dependency resolves them with one map lookup
	// (unchanged fast path). Glob-target rules cannot be keyed, so they are
	// kept in a flat slice and evaluated against every dependency's import path.
	exactRules := make(map[string][]rule.InstRule)
	globRules := make([]targetRule, 0)
	for _, r := range allRules {
		target := r.GetTarget()
		if rule.IsRootTarget(target) {
			if len(sp.rootModulePaths) == 0 && len(sp.buildPackages) > 0 {
				sp.rootModulePaths, err = rootModulePaths(ctx, sp.buildPackages)
				if err != nil {
					return nil, err
				}
			}
			if len(sp.rootModulePaths) == 0 {
				return nil, ex.Newf("rule %q uses target %q, but no root module was found", r.GetName(), target)
			}
			for _, root := range sp.rootModulePaths {
				globRules = append(globRules, targetRule{target: root + "/**", rule: r})
			}
			continue
		}
		if rule.IsGlobTarget(target) {
			globRules = append(globRules, targetRule{target: target, rule: r})
			continue
		}
		exactRules[target] = append(exactRules[target], r)
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
	if len(matched) == 0 {
		_, _ = fmt.Fprintf(os.Stderr, "Warning: no instrumentation will be applied\n")
		sp.Warn("no instrumentation rules matched any dependencies")
	}
	return matched, nil
}
