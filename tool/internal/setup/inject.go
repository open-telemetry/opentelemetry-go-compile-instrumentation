// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"slices"

	"golang.org/x/tools/go/packages"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/rule"
)

// maxInjectionPasses bounds the loop in matchInjectedDeps. Each pass can only
// add hook packages that the previous pass matched, so it settles after one or
// two in practice; the bound is a guard against a cycle, not a real limit.
const maxInjectionPasses = 5

// hookPackagePaths returns the packages otelc.runtime.go blank-imports for the
// given rules, which is every hook package those rules name.
func hookPackagePaths(matched []*rule.InstRuleSet) []string {
	seen := make(map[string]bool)
	paths := make([]string, 0)
	add := func(path string) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		paths = append(paths, path)
	}
	for _, m := range matched {
		for _, r := range m.AllFuncRules() {
			add(r.Path)
		}
		for _, r := range m.FileRules {
			add(r.Path)
		}
	}
	return paths
}

// injectedDeps returns the packages the given hook packages bring into the
// build that known does not already cover.
func injectedDeps(
	ctx context.Context,
	hookPaths []string,
	known map[string]bool,
	dir string,
) ([]*Dependency, error) {
	if len(hookPaths) == 0 {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(ctx, packagesLoadTimeout)
	defer cancel()

	pkgs, err := packages.Load(&packages.Config{
		Mode:    packages.NeedName | packages.NeedFiles | packages.NeedImports | packages.NeedDeps,
		Context: ctx,
		Dir:     dir,
	}, hookPaths...)
	if err != nil {
		return nil, ex.Wrapf(err, "loading hook packages to complete the build plan")
	}

	deps := make([]*Dependency, 0)
	packages.Visit(pkgs, nil, func(pkg *packages.Package) {
		// A package with no Go files of its own has nothing to rewrite, and
		// Sources[0] below would panic.
		if pkg.PkgPath == "" || known[pkg.PkgPath] || len(pkg.GoFiles) == 0 {
			return
		}
		known[pkg.PkgPath] = true
		deps = append(deps, &Dependency{
			ImportPath: pkg.PkgPath,
			Version:    findModVersion(pkg.GoFiles[0]),
			Sources:    slices.Clone(pkg.GoFiles),
			CgoFiles:   map[string]string{},
		})
	})
	return deps, nil
}

// matchInjectedDeps matches rules against the packages that only enter the
// build because otelc put them there, and returns the extra rule sets.
//
// The build plan comes from a dry run of the application as written. Matching
// then decides which hook packages otelc.runtime.go will blank-import, and that
// import drags each hook package's own dependencies into the build. Those
// packages were never in the plan, so a rule targeting one of them has nothing
// to match against.
//
// Nothing reports this. The build succeeds, matched.json is a valid non-empty
// list, and the rule is simply absent from it.
//
// Instrumentation for a third-party library does not hit this, because the
// application imports the library being instrumented, so the plan already
// covers it. It bites a hook package whose rules target its own module, which
// the application has no reason to import.
func (sp *setupPhase) matchInjectedDeps(
	ctx context.Context,
	matched []*rule.InstRuleSet,
	deps []*Dependency,
	moduleDirs map[string]bool,
	dir string,
) ([]*rule.InstRuleSet, error) {
	known := make(map[string]bool, len(deps))
	for _, dep := range deps {
		known[dep.ImportPath] = true
	}

	extra := make([]*rule.InstRuleSet, 0)
	for range maxInjectionPasses {
		injected, err := injectedDeps(ctx, hookPackagePaths(matched), known, dir)
		if err != nil {
			return nil, err
		}
		if len(injected) == 0 {
			break
		}

		sp.Info("Matching dependencies otelc injects into the build", "count", len(injected))
		more, err := sp.matchDeps(ctx, injected, moduleDirs)
		if err != nil {
			return nil, ex.Wrapf(err, "matching injected dependencies to hook rules")
		}
		if len(more) == 0 {
			break
		}

		extra = append(extra, more...)
		// A hook package named only by a rule matched in this pass has not been
		// walked yet, so loop until no further packages appear.
		matched = more
	}
	return extra, nil
}
