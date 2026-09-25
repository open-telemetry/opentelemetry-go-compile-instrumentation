// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"fmt"
	"os"
	"slices"

	"golang.org/x/tools/go/packages"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/pkgload"
	"go.opentelemetry.io/otelc/tool/internal/rule"
)

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
// build that known does not already cover, marking each one in known.
func injectedDeps(
	ctx context.Context,
	hookPaths []string,
	known map[string]bool,
	buildFlags []string,
) ([]*Dependency, error) {
	if len(hookPaths) == 0 {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(ctx, packagesLoadTimeout)
	defer cancel()

	pkgs, err := pkgload.LoadPackages(
		ctx,
		packages.NeedName|packages.NeedFiles|packages.NeedImports|packages.NeedDeps,
		buildFlags,
		hookPaths...,
	)
	if err != nil {
		return nil, ex.Wrapf(err, "loading hook packages to complete the build plan")
	}

	deps := make([]*Dependency, 0)
	var loadErr error
	packages.Visit(pkgs, nil, func(pkg *packages.Package) {
		// packages.Load can return a nil top-level error while reporting
		// failures on individual packages in the closure. Walking past one
		// would match rules against a graph that is silently missing part of
		// what the hook actually imports.
		if len(pkg.Errors) > 0 && loadErr == nil {
			loadErr = ex.Newf("loading package %q: %v", pkg.PkgPath, pkg.Errors[0])
		}
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
			// Known limitation: the plan path reads the cgo mapping out of the
			// compile commands (see findGoSources), which go/packages does not
			// expose here. A rule targeting a cgo package reached only through a
			// hook therefore matches without one. No such rule exists today.
			CgoFiles: map[string]string{},
		})
	})
	if loadErr != nil {
		return nil, loadErr
	}
	return deps, nil
}

// injectionPass is the pair of operations one pass performs. They are supplied
// by the caller so the loop can be exercised without loading or matching real
// packages.
type injectionPass struct {
	load  func(ctx context.Context, hookPaths []string, known map[string]bool) ([]*Dependency, error)
	match func(ctx context.Context, deps []*Dependency) ([]*rule.InstRuleSet, error)
}

// runInjectionPasses repeats load-then-match until a pass turns up nothing new.
func runInjectionPasses(
	ctx context.Context,
	matched []*rule.InstRuleSet,
	known map[string]bool,
	pass injectionPass,
) ([]*rule.InstRuleSet, error) {
	extra := make([]*rule.InstRuleSet, 0)
	for {
		injected, err := pass.load(ctx, hookPackagePaths(matched), known)
		if err != nil {
			return nil, err
		}
		if len(injected) == 0 {
			return extra, nil
		}

		more, err := pass.match(ctx, injected)
		if err != nil {
			return nil, err
		}
		if len(more) == 0 {
			return extra, nil
		}

		extra = append(extra, more...)
		// Only a rule matched in this pass can name a hook package that has not
		// been walked yet, so the next pass starts from what this one found.
		matched = more
	}
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
	buildFlags []string,
) ([]*rule.InstRuleSet, error) {
	known := make(map[string]bool, len(deps))
	for _, dep := range deps {
		known[dep.ImportPath] = true
	}

	pass := injectionPass{
		load: func(ctx context.Context, hookPaths []string, known map[string]bool) ([]*Dependency, error) {
			injected, err := injectedDeps(ctx, hookPaths, known, buildFlags)
			if err != nil {
				// Completing the plan is an improvement on it, not a
				// precondition for building, so a closure that will not resolve
				// leaves the build running on the plan otelc already has. That
				// tradeoff is only honest if it is visible on the console: this
				// is the same silent miss the rest of this change fixes.
				_, _ = fmt.Fprintf(os.Stderr,
					"Warning: could not resolve the packages otelc injects into the build; "+
						"rules targeting them will not be applied: %v\n", err)
				sp.Warn("could not resolve the packages otelc injects into the build; "+
					"rules targeting them will not be applied", "error", err)
				return nil, nil
			}
			if len(injected) > 0 {
				sp.Info("Matching dependencies otelc injects into the build", "count", len(injected))
			}
			return injected, nil
		},
		match: func(ctx context.Context, injected []*Dependency) ([]*rule.InstRuleSet, error) {
			more, err := sp.matchDeps(ctx, injected, moduleDirs)
			if err != nil {
				return nil, ex.Wrapf(err, "matching injected dependencies to hook rules")
			}
			return more, nil
		},
	}

	extra, err := runInjectionPasses(ctx, matched, known, pass)
	if err != nil {
		return nil, err
	}
	return extra, nil
}
