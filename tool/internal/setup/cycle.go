// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"cmp"
	"context"
	"fmt"
	"go/scanner"
	"go/token"
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

// unsafePackageName is the import path of package unsafe.
const unsafePackageName = "unsafe"

// addedImport is an import that a rule adds to the package it instruments.
type addedImport struct {
	rule string
	path string
	// match is set when the rule is listed for every file of the packages its
	// target matches, but adds the import only to some of these files. files
	// are the files to check.
	match *fileMatch
	files []string
}

// addedImportKey identifies an addedImport: the import of path that rule
// adds to pkg, in the files that match finds.
type addedImportKey struct {
	pkg, rule, path string
	match           *fileMatch
}

// fileMatch finds the files that a function_call rule or a literal rule adds
// an import to.
type fileMatch struct {
	// importPath is the package that a changed file imports.
	importPath string
	// text matches the source text of each file that the rule can change. A
	// text search skips most files faster than a parse.
	text *regexp.Regexp
	// changes reports whether the rule adds the import to tree. aliases maps
	// the import names of tree to import paths.
	changes func(tree *dst.File, aliases map[string]string) bool
}

// addedImportGroup is a group of imports that a rule adds, and the files it
// adds them to.
type addedImportGroup struct {
	imports map[string]string
	match   *fileMatch
}

// collectRuleImports returns, for each instrumented package, the imports that
// its matched rules add to it.
func collectRuleImports(matched []*rule.InstRuleSet) map[string][]*addedImport {
	result := make(map[string][]*addedImport)
	seen := make(map[addedImportKey]*addedImport)
	// A rule is listed once for each file it applies to.
	byRule := make(map[rule.InstRule][]addedImportGroup)
	for _, set := range matched {
		pkg := set.ModulePath
		add := func(file string, r rule.InstRule) {
			groups, ok := byRule[r]
			if !ok {
				groups = addedImports(r)
				byRule[r] = groups
			}
			for _, ris := range groups {
				for _, path := range ris.imports {
					// Neither has dependencies.
					if path == "C" || path == unsafePackageName {
						continue
					}
					key := addedImportKey{pkg: pkg, rule: r.GetName(), path: path, match: ris.match}
					ri, found := seen[key]
					if !found {
						ri = &addedImport{rule: r.GetName(), path: path, match: ris.match}
						seen[key] = ri
						result[pkg] = append(result[pkg], ri)
					}
					if ris.match != nil {
						ri.files = append(ri.files, file)
					}
				}
			}
		}
		forEachRule(set.FuncRules, add)
		forEachRule(set.StructRules, add)
		forEachRule(set.RawRules, add)
		forEachRule(set.DirectiveRules, add)
		forEachRule(set.DeclRules, add)
		forEachRule(set.CallRules, add)
		forEachRule(set.LitRules, add)
	}

	for _, imports := range result {
		slices.SortFunc(imports, func(a, b *addedImport) int {
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

// addedImports returns the imports that r adds to the files it applies to.
// Each group tells how to find the files it is added to when r does not add
// it to all of them.
func addedImports(r rule.InstRule) []addedImportGroup {
	switch rt := r.(type) {
	case *rule.InstFuncRule:
		// The hook code of a func rule is not in the target file, so only its
		// blank and dot imports are added there.
		imports := maps.Clone(rt.Imports)
		maps.DeleteFunc(imports, func(alias, _ string) bool { return alias != "_" && alias != "." })
		return []addedImportGroup{{imports: imports}}
	case *rule.InstStructRule:
		types := make([]string, 0, len(rt.NewField))
		for _, f := range rt.NewField {
			types = append(types, f.Type)
		}
		return []addedImportGroup{{imports: referencedImports(rt.Imports, types...)}}
	case *rule.InstRawRule:
		return []addedImportGroup{{imports: referencedImports(rt.Imports, rt.Raw)}}
	case *rule.InstDirectiveRule:
		return []addedImportGroup{{imports: referencedImports(rt.Imports, rt.Template)}}
	case *rule.InstDeclRule:
		return []addedImportGroup{{imports: referencedImports(rt.Imports, rt.Replace, rt.Wrap)}}
	case *rule.InstCallRule:
		// TODO: check method_call rules. To know if a call matches the method,
		// setup must know the type of the receiver, and setup does not
		// type-check packages. A match by the method name only would fail
		// builds that do not have a cycle.
		if rt.MethodCall != "" {
			return nil
		}
		return callImports(rt)
	case *rule.InstLitRule:
		return litImports(rt)
	default:
		return nil
	}
}

// callImports returns the imports that the function_call rule r adds. The
// rule writes variadic_type only into a spread call, as in f(s...), and only
// when it appends arguments. So an import that only variadic_type uses is
// added only to the files with such a call.
func callImports(r *rule.InstCallRule) []addedImportGroup {
	imports := referencedImports(r.Imports, append([]string{r.Replace}, r.AppendArgs...)...)
	groups := []addedImportGroup{{imports: imports, match: callMatch(r, false)}}
	if len(r.AppendArgs) == 0 {
		return groups
	}
	spread := referencedImports(r.Imports, r.VariadicType)
	maps.DeleteFunc(spread, func(alias, _ string) bool {
		_, found := imports[alias]
		return found
	})
	if len(spread) > 0 {
		groups = append(groups, addedImportGroup{imports: spread, match: callMatch(r, true)})
	}
	return groups
}

// litImports returns the imports that the literal rule r adds. Each literal
// gets only the code of the fields that apply to it, so each import is added
// only to the files with a literal that gets code that uses the import.
func litImports(r *rule.InstLitRule) []addedImportGroup {
	code := make([]string, 0, len(r.Field))
	for _, f := range r.Field {
		code = append(code, f.Value, f.Wrap)
	}
	used := referencedImports(r.Imports, code...)
	groups := make([]addedImportGroup, 0, len(used))
	blankAndDot := make(map[string]string)
	for _, alias := range slices.Sorted(maps.Keys(used)) {
		if alias == "_" || alias == "." {
			blankAndDot[alias] = used[alias]
			continue
		}
		groups = append(groups, addedImportGroup{
			imports: map[string]string{alias: used[alias]},
			match:   litMatch(r, alias),
		})
	}
	if len(blankAndDot) > 0 {
		groups = append(groups, addedImportGroup{imports: blankAndDot, match: litMatch(r, "")})
	}
	return groups
}

// referencedImports returns the blank and dot imports in imports, and the
// imports whose name code selects from. The instrumentation does not add an
// import that the rewritten file does not use.
func referencedImports(imports map[string]string, code ...string) map[string]string {
	selected := make(map[string]bool)
	for _, src := range code {
		addSelectedNames(src, selected)
	}
	used := maps.Clone(imports)
	maps.DeleteFunc(used, func(alias, _ string) bool {
		return alias != "_" && alias != "." && !selected[alias]
	})
	return used
}

// addSelectedNames adds to names each identifier that src selects from, as in
// name.X. src can contain template actions, so it is scanned and not parsed.
// The scanner skips strings and comments.
func addSelectedNames(src string, names map[string]bool) {
	if src == "" {
		return
	}
	var s scanner.Scanner
	file := token.NewFileSet().AddFile("", -1, len(src))
	s.Init(file, []byte(src), nil, 0)
	prev := ""
	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			return
		}
		if tok == token.PERIOD && prev != "" {
			names[prev] = true
		}
		prev = ""
		if tok == token.IDENT {
			prev = lit
		}
	}
}

// callMatch returns how to find the files that the function_call rule r
// changes: the files with a call of the function, at a call site without
// //otelc:ignore. With spread, only a spread call, as in f(s...), counts.
func callMatch(r *rule.InstCallRule, spread bool) *fileMatch {
	return &fileMatch{
		importPath: r.ImportPath,
		text:       regexp.MustCompile(`\.\s*` + regexp.QuoteMeta(r.FuncName) + `\b`),
		changes: func(tree *dst.File, aliases map[string]string) bool {
			stmts := ast.CallEnclosingStmts(tree)
			return containsNode(tree, func(node dst.Node) bool {
				call, ok := node.(*dst.CallExpr)
				if !ok || (spread && !call.Ellipsis) {
					return false
				}
				sel, ok := call.Fun.(*dst.SelectorExpr)
				if !ok || sel.Sel.Name != r.FuncName {
					return false
				}
				ident, ok := sel.X.(*dst.Ident)
				return ok && aliases[ident.Name] == r.ImportPath &&
					!ast.HasLeadingDirective(stmts[call], util.DirectiveIgnore)
			})
		},
	}
}

// litMatch returns how to find the files that the literal rule r adds the
// import named alias to: the files with a keyed or empty literal of the rule's
// type that gets code from the rule that selects from alias. With an empty
// alias, any code counts.
func litMatch(r *rule.InstLitRule, alias string) *fileMatch {
	return &fileMatch{
		importPath: r.ImportPath,
		text:       regexp.MustCompile(`\.\s*` + regexp.QuoteMeta(r.TypeName) + `\s*\{`),
		changes: func(tree *dst.File, aliases map[string]string) bool {
			return containsNode(tree, func(node dst.Node) bool {
				lit, ok := node.(*dst.CompositeLit)
				return ok && isRuleLiteral(lit, r, aliases) && changesLiteral(lit, r, alias)
			})
		},
	}
}

// isRuleLiteral reports whether lit builds the type of the literal rule r.
// Only a literal with a qualified type, as in pkg.Type{}, matches.
func isRuleLiteral(lit *dst.CompositeLit, r *rule.InstLitRule, aliases map[string]string) bool {
	sel, ok := lit.Type.(*dst.SelectorExpr)
	if !ok || sel.Sel.Name != r.TypeName {
		return false
	}
	ident, ok := sel.X.(*dst.Ident)
	if !ok {
		return false
	}
	if ident.Path != "" {
		return ident.Path == r.ImportPath
	}
	return aliases[ident.Name] == r.ImportPath
}

// changesLiteral reports whether the literal rule r writes code into lit that
// selects from the import name alias, or any code when alias is empty. The
// rule skips a literal with positional elements.
func changesLiteral(lit *dst.CompositeLit, r *rule.InstLitRule, alias string) bool {
	keys := make(map[string]bool, len(lit.Elts))
	for _, elt := range lit.Elts {
		kv, ok := elt.(*dst.KeyValueExpr)
		if !ok {
			return false
		}
		if key, isIdent := kv.Key.(*dst.Ident); isIdent {
			keys[key.Name] = true
		}
	}
	return slices.ContainsFunc(r.Field, func(f *rule.InstLitField) bool {
		code := appliedCode(f, keys[f.Name])
		return code != "" && (alias == "" || selectsFrom(code, alias))
	})
}

// appliedCode returns the code that the field f writes into a literal: its
// wrap when the literal sets the field and f has a wrap, else its value. set
// reports whether the literal sets the field. A wrap does not change a
// literal that does not set the field.
func appliedCode(f *rule.InstLitField, set bool) string {
	if set && f.Wrap != "" {
		return f.Wrap
	}
	return f.Value
}

// selectsFrom reports whether code selects from name, as in name.X.
func selectsFrom(code, name string) bool {
	names := make(map[string]bool)
	addSelectedNames(code, names)
	return names[name]
}

// containsNode reports whether a node in tree satisfies match.
func containsNode(tree *dst.File, match func(dst.Node) bool) bool {
	found := false
	dst.Inspect(tree, func(node dst.Node) bool {
		if !found && match(node) {
			found = true
		}
		return !found
	})
	return found
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
		pkgs, err := pkgload.LoadPackages(ctx,
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
func (sp *setupPhase) findRuleImportCycle(graph map[string][]string, added map[string][]*addedImport) error {
	s := &cycleSearch{
		graph:  graph,
		added:  added,
		adds:   func(pkg string, ri *addedImport) (bool, error) { return sp.addsImport(graph, pkg, ri) },
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
func (sp *setupPhase) addsImport(graph map[string][]string, pkg string, ri *addedImport) (bool, error) {
	if ri.match == nil {
		return true, nil
	}
	// A changed file refers to the package of the function or the type
	// through an import of it.
	if _, ok := slices.BinarySearch(graph[pkg], ri.match.importPath); !ok {
		return false, nil
	}
	for _, file := range ri.files {
		changes, err := ri.match.changesFile(file, sp.resolvedNames)
		if err != nil || changes {
			return changes, err
		}
	}
	return false, nil
}

// changesFile reports whether the rule of m changes file. names maps import
// paths to package names.
func (m *fileMatch) changesFile(file string, names map[string]string) (bool, error) {
	content, err := os.ReadFile(file)
	if err != nil {
		return false, ex.Wrapf(err, "reading %s", file)
	}
	if !m.text.Match(content) {
		return false, nil
	}
	tree, err := ast.ParseFileFast(file)
	if err != nil {
		return false, err
	}
	return m.changes(tree, ast.ImportAliasMap(tree, names)), nil
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
	added map[string][]*addedImport
	adds  func(pkg string, ri *addedImport) (bool, error)
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
