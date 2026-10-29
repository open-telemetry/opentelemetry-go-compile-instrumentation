// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/dave/dst"
	"golang.org/x/tools/go/packages"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/pkgload"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

// ruleImport is an import that a rule adds to the package it instruments.
type ruleImport struct {
	rule string
	path string
	// call is set when the rule is a function_call rule. Such a rule is listed
	// for every file of the packages its target matches, but it adds its
	// imports only to a file that calls the function. files are the files to
	// check for a call.
	call  *rule.InstCallRule
	files []string
}

// collectRuleImports returns, for each instrumented package, the imports that
// its matched rules add to it.
func collectRuleImports(matched []*rule.InstRuleSet) map[string][]*ruleImport {
	result := make(map[string][]*ruleImport)
	seen := make(map[[3]string]*ruleImport)
	for _, set := range matched {
		pkg := set.ModulePath
		add := func(file string, r rule.InstRule) {
			imports, call := addedImports(r)
			for _, path := range imports {
				// Neither has dependencies.
				if path == "C" || path == "unsafe" {
					continue
				}
				key := [3]string{pkg, r.GetName(), path}
				ri, ok := seen[key]
				if !ok {
					ri = &ruleImport{rule: r.GetName(), path: path, call: call}
					seen[key] = ri
					result[pkg] = append(result[pkg], ri)
				}
				if call != nil {
					ri.files = append(ri.files, file)
				}
			}
		}
		// Literal rules are not checked: whether one changes a literal is only
		// known when the rule is applied.
		forEachRule(set.FuncRules, add)
		forEachRule(set.StructRules, add)
		forEachRule(set.RawRules, add)
		forEachRule(set.DirectiveRules, add)
		forEachRule(set.DeclRules, add)
		forEachRule(set.CallRules, add)
	}

	for _, imports := range result {
		slices.SortFunc(imports, func(a, b *ruleImport) int {
			return cmp.Or(cmp.Compare(a.path, b.path), cmp.Compare(a.rule, b.rule))
		})
		for _, ri := range imports {
			slices.Sort(ri.files)
		}
	}
	return result
}

// forEachRule calls fn for each rule in rules and the file it applies to.
func forEachRule[T rule.InstRule](rules map[string][]T, fn func(file string, r rule.InstRule)) {
	for file, rs := range rules {
		for _, r := range rs {
			fn(file, r)
		}
	}
}

// addedImports returns the imports that r adds to the files it applies to. It
// also returns r when r is a function_call rule.
func addedImports(r rule.InstRule) (map[string]string, *rule.InstCallRule) {
	switch rt := r.(type) {
	case *rule.InstFuncRule:
		// The hook code of a func rule is not in the target file, so only its
		// blank and dot imports are added there.
		imports := maps.Clone(rt.Imports)
		maps.DeleteFunc(imports, func(alias, _ string) bool { return alias != "_" && alias != "." })
		return imports, nil
	case *rule.InstStructRule:
		return rt.Imports, nil
	case *rule.InstRawRule:
		return rt.Imports, nil
	case *rule.InstDirectiveRule:
		return rt.Imports, nil
	case *rule.InstDeclRule:
		return rt.Imports, nil
	case *rule.InstCallRule:
		// A method call is only resolved when the rule is applied, with type
		// information that setup does not have.
		if rt.MethodCall != "" {
			return nil, nil
		}
		return rt.Imports, rt
	default:
		return nil, nil
	}
}

// addImportGraph adds the imports of pkgs and of their dependencies to graph,
// which maps an import path to the sorted import paths it imports. The
// variants of a package in a test build merge into one entry.
func addImportGraph(graph map[string][]string, pkgs []*packages.Package) {
	packages.Visit(pkgs, nil, func(pkg *packages.Package) {
		imports := graph[pkg.PkgPath]
		for _, imp := range pkg.Imports {
			imports = append(imports, imp.PkgPath)
		}
		slices.Sort(imports)
		graph[pkg.PkgPath] = slices.Compact(imports)
	})
}

// checkRuleImportCycles fails when the imports that the matched rules add
// would make a package depend on itself. A compile only sees its own imports,
// so it does not notice such a cycle, and the link then fails with an error
// that names neither the rule nor the cycle.
//
// buildPkgs are the packages of the build with their dependencies.
func (sp *setupPhase) checkRuleImportCycles(
	ctx context.Context,
	matched []*rule.InstRuleSet,
	buildPkgs []*packages.Package,
) error {
	added := collectRuleImports(matched)
	if len(added) == 0 {
		return nil
	}

	graph := make(map[string][]string)
	addImportGraph(graph, buildPkgs)

	// A rule can import a package that the build does not have yet. Every
	// cycle goes through a rule import, so the dependencies of these packages
	// complete the graph.
	var missing []string
	for _, imports := range added {
		for _, ri := range imports {
			if _, ok := graph[ri.path]; !ok && !slices.Contains(missing, ri.path) {
				missing = append(missing, ri.path)
			}
		}
	}
	if len(missing) > 0 {
		loadCtx, cancel := context.WithTimeout(ctx, packagesLoadTimeout)
		defer cancel()
		pkgs, err := pkgload.LoadPackages(loadCtx,
			packages.NeedName|packages.NeedImports|packages.NeedDeps, sp.buildFlags, false, missing...)
		if err != nil {
			// The check only improves the error of a build that would fail
			// later, so it must not fail a build on its own.
			sp.Warn("skipping the import cycle check of rule imports", "error", err)
			return nil
		}
		addImportGraph(graph, pkgs)
	}

	return sp.findRuleImportCycle(graph, added)
}

// findRuleImportCycle returns an error that describes the first cycle that the
// rule imports in added form with the imports in graph.
func (sp *setupPhase) findRuleImportCycle(graph map[string][]string, added map[string][]*ruleImport) error {
	s := &cycleSearch{
		graph:  graph,
		added:  added,
		adds:   func(pkg string, ri *ruleImport) (bool, error) { return sp.addsImport(graph, pkg, ri) },
		done:   make(map[string]bool),
		onPath: make(map[string]int),
	}

	// Go rejects a cycle in the source code imports, so every cycle has a rule
	// import, and the package it imports is on the cycle.
	starts := make([]string, 0)
	for _, imports := range added {
		for _, ri := range imports {
			starts = append(starts, ri.path)
		}
	}
	slices.Sort(starts)
	for _, start := range slices.Compact(starts) {
		if s.done[start] {
			continue
		}
		cycle, err := s.visit(start)
		if err != nil {
			return err
		}
		if cycle != nil {
			return describeImportCycle(cycle)
		}
	}
	return nil
}

// addsImport reports whether ri is added to pkg.
func (sp *setupPhase) addsImport(graph map[string][]string, pkg string, ri *ruleImport) (bool, error) {
	if ri.call == nil {
		return true, nil
	}
	// A file calls the function through an import of its package.
	if _, ok := slices.BinarySearch(graph[pkg], ri.call.ImportPath); !ok {
		return false, nil
	}
	// Most files never select the function's name, and a text search skips
	// them faster than a parse.
	selector := regexp.MustCompile(`\.\s*` + regexp.QuoteMeta(ri.call.FuncName) + `\b`)
	for _, file := range ri.files {
		calls, err := callsFunction(file, ri.call, selector, sp.resolvedNames)
		if err != nil || calls {
			return calls, err
		}
	}
	return false, nil
}

// callsFunction reports whether file calls the function of the function_call
// rule r at a call site without //otelc:ignore. selector matches the
// selection of the function's name in the source text.
func callsFunction(file string, r *rule.InstCallRule, selector *regexp.Regexp, names map[string]string) (bool, error) {
	content, err := os.ReadFile(file)
	if err != nil {
		return false, ex.Wrapf(err, "reading %s", file)
	}
	if !selector.Match(content) {
		return false, nil
	}
	tree, err := ast.ParseFileFast(file)
	if err != nil {
		return false, err
	}
	aliases := ast.ImportAliasMap(tree, names)
	stmts := ast.CallEnclosingStmts(tree)
	found := false
	dst.Inspect(tree, func(node dst.Node) bool {
		if found {
			return false
		}
		call, ok := node.(*dst.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*dst.SelectorExpr)
		if !ok || sel.Sel.Name != r.FuncName {
			return true
		}
		ident, ok := sel.X.(*dst.Ident)
		found = ok && aliases[ident.Name] == r.ImportPath &&
			!ast.HasLeadingDirective(stmts[call], util.DirectiveIgnore)
		return !found
	})
	return found, nil
}

// importEdge is an import of one package by another. rule names the rule that
// adds the import, and is empty for an import in the source code.
type importEdge struct {
	from, to, rule string
}

// cycleSearch is a depth-first search for a cycle in the imports of graph and
// added. It visits each package at most once.
type cycleSearch struct {
	graph map[string][]string
	added map[string][]*ruleImport
	adds  func(pkg string, ri *ruleImport) (bool, error)
	done  map[string]bool
	// onPath maps each package on the current path to the index in path of
	// its first edge.
	onPath map[string]int
	path   []importEdge
}

// visit returns the edges of a cycle that is reachable from pkg, or nil.
func (s *cycleSearch) visit(pkg string) ([]importEdge, error) {
	s.onPath[pkg] = len(s.path)
	defer func() {
		delete(s.onPath, pkg)
		s.done[pkg] = true
	}()

	edges := make([]importEdge, 0, len(s.graph[pkg])+len(s.added[pkg]))
	for _, imp := range s.graph[pkg] {
		edges = append(edges, importEdge{from: pkg, to: imp})
	}
	for _, ri := range s.added[pkg] {
		adds, err := s.adds(pkg, ri)
		if err != nil {
			return nil, err
		}
		if adds {
			edges = append(edges, importEdge{from: pkg, to: ri.path, rule: ri.rule})
		}
	}

	for _, e := range edges {
		if start, ok := s.onPath[e.to]; ok {
			return append(slices.Clone(s.path[start:]), e), nil
		}
		if s.done[e.to] {
			continue
		}
		s.path = append(s.path, e)
		cycle, err := s.visit(e.to)
		s.path = s.path[:len(s.path)-1]
		if cycle != nil || err != nil {
			return cycle, err
		}
	}
	return nil, nil
}

// describeImportCycle returns an error that names the first rule import of
// cycle, and the imports that lead from it back to the package it is added to.
func describeImportCycle(cycle []importEdge) error {
	i := slices.IndexFunc(cycle, func(e importEdge) bool { return e.rule != "" })
	if i < 0 {
		return ex.Newf("import cycle: %s", describeImportPath(cycle[0].from, cycle))
	}
	added := cycle[i]
	rest := slices.Concat(cycle[i+1:], cycle[:i])
	switch {
	case len(rest) == 0:
		return ex.Newf("rule %q adds import %q to the package itself", added.rule, added.to)
	case len(rest) == 1 && rest[0].rule == "":
		return ex.Newf("rule %q adds import %q to %q, but %s imports %s",
			added.rule, added.to, added.from, added.to, added.from)
	default:
		return ex.Newf("rule %q adds import %q to %q, but %s depends on %s: %s",
			added.rule, added.to, added.from, added.to, added.from, describeImportPath(added.to, rest))
	}
}

// describeImportPath returns the packages of the edges that lead from start,
// and the rules that add the imports between them.
func describeImportPath(start string, edges []importEdge) string {
	steps := make([]string, 0, len(edges)+1)
	steps = append(steps, start)
	for _, e := range edges {
		step := e.to
		if e.rule != "" {
			step += fmt.Sprintf(" (added by rule %q)", e.rule)
		}
		steps = append(steps, step)
	}
	return strings.Join(steps, " -> ")
}
