// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"

	"go.opentelemetry.io/otelc/tool/internal/rule"
)

// mainPackagePath is the import path the build plan gives every main package,
// so the rule sets that target a main package carry it.
const mainPackagePath = "main"

// runtimeScope selects, for each package that receives an otelc.runtime.go, the
// matched rule sets whose hooks its binaries need.
type runtimeScope struct {
	matched []*rule.InstRuleSet
	// added maps each rule set to the packages its rules add to the build:
	// hook packages, add_file source imports, imports entries and
	// requiredImports.
	added [][]ruleImport
	// deps maps each added package to the packages it depends on, itself
	// included.
	deps map[string]map[string]bool
	// testPkgs holds the test variants and test binaries of a go test build.
	testPkgs []*packages.Package
	testMode bool
}

// newRuntimeScope returns the scope for the given build packages, or nil when
// every package receives every matched rule set: with a single build package,
// all matched rule sets already come from its build, and if the packages that
// rules add cannot be loaded, the scope cannot be computed.
func (sp *setupPhase) newRuntimeScope(
	ctx context.Context,
	pkgs []*packages.Package,
	matched []*rule.InstRuleSet,
	hookDeps map[string]map[string]bool,
) *runtimeScope {
	if len(pkgs) < 2 || len(matched) == 0 {
		return nil
	}

	added := make([][]ruleImport, len(matched))
	all := make(map[string]bool)
	for i, rs := range matched {
		added[i] = sp.ruleSetAdditions(rs)
		for _, imp := range added[i] {
			all[imp.path] = true
		}
	}

	deps, err := sp.addedPackageDeps(ctx, all, hookDeps)
	if err != nil {
		sp.Debug("cannot load the packages that rules add; every build package links every matched rule",
			"error", err)
		return nil
	}

	return &runtimeScope{
		matched:  matched,
		added:    added,
		deps:     deps,
		testPkgs: sp.testPackages,
		testMode: sp.testMode,
	}
}

// ruleImport is a package that a rule adds to the build. When requires is set,
// the rule only adds it in a binary that builds requires.
type ruleImport struct {
	path     string
	requires string
}

// ruleSetAdditions returns the packages that the rules of rs add to the build.
//
// Hook packages and the imports of most rules come with the rule set. A func
// rule also brings in requiredImports, which otelc.runtime.go imports when it
// declares func rule hooks. A call rule or a literal rule only applies where
// its function is called or its type is used, which setup cannot see: a rule
// with a "**" target is attached to every package. Its imports therefore
// require the package of that function or type, since a binary without it has
// nothing for the rule to rewrite.
func (sp *setupPhase) ruleSetAdditions(rs *rule.InstRuleSet) []ruleImport {
	seen := make(map[ruleImport]bool)
	var imports []ruleImport
	add := func(ruleImports map[string]string, requires string) {
		for _, path := range ruleImports {
			imp := ruleImport{path: path, requires: requires}
			if path == "" || seen[imp] {
				continue
			}
			seen[imp] = true
			imports = append(imports, imp)
		}
	}
	for _, path := range sp.hookPackagePaths([]*rule.InstRuleSet{rs}) {
		add(map[string]string{"": path}, "")
	}
	if len(rs.AllFuncRules()) > 0 {
		for path := range requiredImports {
			add(map[string]string{"": path}, "")
		}
	}
	addRuleImports(rs.RawRules, add, func(r *rule.InstRawRule) (map[string]string, string) { return r.Imports, "" })
	addRuleImports(rs.FuncRules, add, func(r *rule.InstFuncRule) (map[string]string, string) { return r.Imports, "" })
	addRuleImports(rs.StructRules, add, func(r *rule.InstStructRule) (map[string]string, string) {
		return r.Imports, ""
	})
	addRuleImports(rs.DirectiveRules, add, func(r *rule.InstDirectiveRule) (map[string]string, string) {
		return r.Imports, ""
	})
	addRuleImports(rs.DeclRules, add, func(r *rule.InstDeclRule) (map[string]string, string) { return r.Imports, "" })
	addRuleImports(rs.CallRules, add, func(r *rule.InstCallRule) (map[string]string, string) {
		return r.Imports, r.ImportPath
	})
	addRuleImports(rs.LitRules, add, func(r *rule.InstLitRule) (map[string]string, string) {
		return r.Imports, r.ImportPath
	})
	for _, r := range rs.FileRules {
		add(r.Imports, "")
	}
	slices.SortFunc(imports, func(a, b ruleImport) int {
		if c := strings.Compare(a.path, b.path); c != 0 {
			return c
		}
		return strings.Compare(a.requires, b.requires)
	})
	return imports
}

// addRuleImports passes the imports of every rule in byFile to add, with the
// package that each rule requires.
func addRuleImports[R any](
	byFile map[string][]R,
	add func(imports map[string]string, requires string),
	importsOf func(R) (map[string]string, string),
) {
	for _, rules := range byFile {
		for _, r := range rules {
			add(importsOf(r))
		}
	}
}

// testPackages returns the packages of allPkgs that buildPkgs leaves out: the
// test variants and the test binaries of a go test build.
func testPackages(buildPkgs, allPkgs []*packages.Package) []*packages.Package {
	built := make(map[string]bool, len(buildPkgs))
	for _, pkg := range buildPkgs {
		built[pkg.ID] = true
	}
	var tests []*packages.Package
	for _, pkg := range allPkgs {
		if !built[pkg.ID] {
			tests = append(tests, pkg)
		}
	}
	return tests
}

// addedPackageDeps returns the dependencies of each package in paths, itself
// included. Hook packages reuse hookDeps, which hookImports already loaded.
func (sp *setupPhase) addedPackageDeps(
	ctx context.Context,
	paths map[string]bool,
	hookDeps map[string]map[string]bool,
) (map[string]map[string]bool, error) {
	deps := make(map[string]map[string]bool, len(paths))
	var missing []string
	for p := range paths {
		if d, ok := hookDeps[p]; ok {
			deps[p] = d
			continue
		}
		missing = append(missing, p)
	}
	if len(missing) == 0 {
		return deps, nil
	}

	slices.Sort(missing)
	loaded, err := sp.loadGraph(ctx, sp.buildFlags, missing)
	if err != nil {
		return nil, err
	}
	var loadErr error
	for _, pkg := range loaded {
		d := make(map[string]bool)
		packages.Visit([]*packages.Package{pkg}, nil, func(p *packages.Package) {
			if len(p.Errors) > 0 && loadErr == nil {
				loadErr = p.Errors[0]
			}
			d[p.PkgPath] = true
		})
		deps[pkg.PkgPath] = d
	}
	if loadErr != nil {
		return nil, loadErr
	}
	return deps, nil
}

// runtimeRuleSets holds the rule sets whose hooks the runtime files of one
// package import.
type runtimeRuleSets struct {
	// pkg holds the rule sets of otelc.runtime.go. Every binary that builds
	// the package links that file.
	pkg []*rule.InstRuleSet
	// test holds the rule sets that only the test binary of the package needs.
	// Their hooks go to otelc.runtime_test.go, which only that binary links.
	test []*rule.InstRuleSet
}

// ruleSets returns the rule sets whose hooks the binaries of pkg need, in the
// order of the matched rule sets. importPath is the import path the runtime
// file of pkg is compiled under.
func (s *runtimeScope) ruleSets(pkg *packages.Package, importPath string) runtimeRuleSets {
	if s == nil {
		return runtimeRuleSets{}
	}
	sets := selectRuleSets(s.matched, s.added, s.deps, reachable([]*packages.Package{pkg}, importPath))
	if !s.testMode {
		return runtimeRuleSets{pkg: sets}
	}

	inPkg := make(map[*rule.InstRuleSet]bool, len(sets))
	for _, rs := range sets {
		inPkg[rs] = true
	}
	var test []*rule.InstRuleSet
	withTests := append([]*packages.Package{pkg}, s.testRoots(pkg)...)
	for _, rs := range selectRuleSets(s.matched, s.added, s.deps, reachable(withTests, importPath)) {
		if !inPkg[rs] {
			test = append(test, rs)
		}
	}
	return runtimeRuleSets{pkg: sets, test: test}
}

// testRoots returns the test variants and the test binary of pkg.
func (s *runtimeScope) testRoots(pkg *packages.Package) []*packages.Package {
	var roots []*packages.Package
	for _, tp := range s.testPkgs {
		if tp.ForTest == pkg.PkgPath || tp.PkgPath == pkg.PkgPath+".test" {
			roots = append(roots, tp)
		}
	}
	return roots
}

// reachable returns the packages that a binary with roots builds: roots and
// their dependencies. importPath is the import path the runtime file is
// compiled under.
func reachable(roots []*packages.Package, importPath string) map[string]bool {
	built := map[string]bool{importPath: true, mainPackagePath: true}
	packages.Visit(roots, nil, func(p *packages.Package) {
		built[p.PkgPath] = true
	})
	return built
}

// selectRuleSets returns the rule sets that target a package in reachable. The
// packages a selected rule set adds to the build are built too, so their
// dependencies join reachable and can select more rule sets. A package that a
// rule only adds next to another package joins once that package does.
func selectRuleSets(
	matched []*rule.InstRuleSet,
	added [][]ruleImport,
	deps map[string]map[string]bool,
	reachable map[string]bool,
) []*rule.InstRuleSet {
	selected := make([]bool, len(matched))
	done := make([][]bool, len(matched))
	for changed := true; changed; {
		changed = false
		for i, rs := range matched {
			if !selected[i] {
				if !reachable[rs.ModulePath] {
					continue
				}
				selected[i] = true
				done[i] = make([]bool, len(added[i]))
				changed = true
			}
			for j, imp := range added[i] {
				if done[i][j] || (imp.requires != "" && !reachable[imp.requires]) {
					continue
				}
				done[i][j] = true
				changed = true
				reachable[imp.path] = true
				for d := range deps[imp.path] {
					reachable[d] = true
				}
			}
		}
	}

	result := make([]*rule.InstRuleSet, 0, len(matched))
	for i, rs := range matched {
		if selected[i] {
			result = append(result, rs)
		}
	}
	return result
}
