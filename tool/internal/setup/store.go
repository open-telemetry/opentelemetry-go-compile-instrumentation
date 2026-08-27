// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"encoding/json"
	"maps"
	"slices"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
	"golang.org/x/tools/go/packages"
)

// resolveRulePaths resolves the import paths referenced by function and file rules
// to absolute filesystem paths.
//
// This must be done during the setup phase because the instrument phase no longer
// has enough context to resolve import paths (module directories). The resolved paths
// are embedded into the rules and consumed directly during instrumentation.
//
// All pending paths are collected up front and resolved together, batching
// them into a single packages.Load call per module dir instead of issuing
// one call per path.
func resolveRulePaths(ctx context.Context, matched []*rule.InstRuleSet, moduleDirs map[string]bool) error {
	dirs := slices.Sorted(maps.Keys(moduleDirs))

	var pending []string
	for _, ruleset := range matched {
		for _, fileRule := range ruleset.FileRules {
			pending = append(pending, fileRule.Path)
		}
		for _, funcRule := range ruleset.AllFuncRules() {
			pending = append(pending, funcRule.Path)
		}
	}
	slices.Sort(pending)
	pending = slices.Compact(pending)

	resolved := make(map[string]string, len(pending))
	var lastErr error

	for _, moduleDir := range dirs {
		if len(pending) == 0 {
			break
		}

		pkgs, loadErr := packages.Load(&packages.Config{
			Mode:    packages.NeedName | packages.NeedFiles,
			Context: ctx,
			Dir:     moduleDir,
		}, pending...)
		if loadErr != nil {
			lastErr = loadErr
			continue
		}

		next := pending[:0]
		for _, p := range pending {
			pkg, err := findPackage(pkgs, p)
			switch {
			case err != nil:
				lastErr = err
			case pkg == nil:
				next = append(next, p)
			default:
				resolved[p] = pkg.Dir
			}
		}
		pending = next
	}

	if len(pending) > 0 {
		return ex.Wrapf(lastErr, "failed to resolve import path %q", pending[0])
	}

	for _, ruleset := range matched {
		for _, fileRule := range ruleset.FileRules {
			fileRule.ResolvedPath = resolved[fileRule.Path]
		}
		for _, funcRule := range ruleset.AllFuncRules() {
			funcRule.ResolvedPath = resolved[funcRule.Path]
		}
	}

	return nil
}

// findPackage returns the single package in pkgs matching importPath, nil if
// none matched (the caller should retry it in the next module dir), or an
// error if importPath ambiguously matched more than one package.
func findPackage(pkgs []*packages.Package, importPath string) (*packages.Package, error) {
	var found *packages.Package
	for _, pkg := range pkgs {
		if pkg.PkgPath != importPath || len(pkg.Errors) > 0 || pkg.Dir == "" {
			continue
		}
		if found != nil {
			return nil, ex.Newf("import path %q resolved to multiple packages", importPath)
		}
		found = pkg
	}
	return found, nil
}

// store stores the matched rules to the file
// It's the pair of the InstrumentPhase.load
func (sp *setupPhase) store(ctx context.Context, matched []*rule.InstRuleSet, moduleDirs map[string]bool) error {
	if err := resolveRulePaths(ctx, matched, moduleDirs); err != nil {
		return ex.Wrapf(err, "resolving rule paths")
	}

	bs, err := json.Marshal(matched)
	if err != nil {
		return ex.Wrapf(err, "failed to marshal rules to JSON")
	}

	f := util.GetMatchedRuleFile()
	err = util.WriteFileAtomic(f, bs)
	if err != nil {
		return ex.Wrapf(err, "failed to write matched rules to file %s", f)
	}
	sp.Info("Stored matched sets", "path", f)
	return nil
}
