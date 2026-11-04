// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	goparser "go/parser"
	"go/types"
	"os"
	"path/filepath"
	"regexp"

	"github.com/dave/dst"
	"golang.org/x/tools/go/packages"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/instrument"
	"go.opentelemetry.io/otelc/tool/internal/pkgload"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

// methodMatch finds the calls of the method of a method_call rule.
type methodMatch struct {
	rule *rule.InstCallRule
	// text matches the source text of each file with a call of the method. A
	// text search skips most files faster than a parse.
	text *regexp.Regexp
}

func newMethodMatch(r *rule.InstCallRule) *methodMatch {
	return &methodMatch{
		rule: r,
		text: regexp.MustCompile(`\.\s*` + regexp.QuoteMeta(r.FuncName) + `\b`),
	}
}

// sourcePosition is the position of a node in a source file. file is the base
// name of the file.
type sourcePosition struct {
	file      string
	line, col int
}

// callPositions returns the positions of the method names in the calls of
// file that can call the method of m: the calls of a method with the same
// name, at a call site without //otelc:ignore.
func (m *methodMatch) callPositions(file string) ([]sourcePosition, error) {
	content, err := os.ReadFile(file)
	if err != nil {
		return nil, ex.Wrapf(err, "reading %s", file)
	}
	if !m.text.Match(content) {
		return nil, nil
	}
	parser := ast.NewAstParser()
	tree, err := parser.Parse(file, goparser.SkipObjectResolution|goparser.ParseComments)
	if err != nil {
		return nil, err
	}
	stmts := ast.CallEnclosingStmts(tree)
	var positions []sourcePosition
	dst.Inspect(tree, func(node dst.Node) bool {
		call, ok := node.(*dst.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*dst.SelectorExpr)
		if !ok || sel.Sel.Name != m.rule.FuncName || ast.HasLeadingDirective(stmts[call], util.DirectiveIgnore) {
			return true
		}
		pos := parser.FindPosition(sel.Sel)
		positions = append(positions, sourcePosition{
			file: filepath.Base(pos.Filename),
			line: pos.Line,
			col:  pos.Column,
		})
		return true
	})
	return positions, nil
}

// methodCheck reports whether a method_call rule adds its imports to a
// package. Only the types of the package tell which method a call calls, so
// methodCheck type-checks the package. It does so only when the import that
// the rule adds can close a cycle, which is rare.
type methodCheck struct {
	sp    *setupPhase
	graph map[string][]string
	added map[string][]*ruleImport
	// deps maps a package to the packages that it imports directly or
	// indirectly in the source code.
	deps map[string]map[string]bool
	// reach maps a package to the packages that it imports directly or
	// indirectly in the source code or through any rule import.
	reach map[string]map[string]bool
	// selections maps a package to the selections in its source code, or to
	// nil when the type check failed.
	selections map[string]map[sourcePosition]*types.Selection
}

func newMethodCheck(sp *setupPhase, graph map[string][]string, added map[string][]*ruleImport) *methodCheck {
	return &methodCheck{
		sp:         sp,
		graph:      graph,
		added:      added,
		deps:       make(map[string]map[string]bool),
		reach:      make(map[string]map[string]bool),
		selections: make(map[string]map[sourcePosition]*types.Selection),
	}
}

// adds reports whether the method_call rule of ri adds ri to pkg.
func (c *methodCheck) adds(ctx context.Context, pkg string, ri *ruleImport) (bool, error) {
	r := ri.method.rule
	// A package can only call a method of a type of its own package or of a
	// package it depends on.
	if pkg != r.ImportPath && !reachable(c.deps, c.graph, nil, pkg, r.ImportPath) {
		return false, nil
	}
	// An import that is not on a cycle does not change the result of the
	// search, so the type check is not necessary.
	if ri.path != pkg && !reachable(c.reach, c.graph, c.added, ri.path, pkg) {
		return false, nil
	}

	var calls []sourcePosition
	for _, file := range ri.files {
		positions, err := ri.method.callPositions(file)
		if err != nil {
			return false, err
		}
		calls = append(calls, positions...)
	}
	if len(calls) == 0 {
		return false, nil
	}

	selections := c.packageSelections(ctx, pkg)
	for _, pos := range calls {
		selection, ok := selections[pos]
		if !ok {
			continue
		}
		path, recvType, ok := instrument.MethodReceiver(selection)
		if ok && path == r.ImportPath && recvType == r.RecvType {
			return true, nil
		}
	}
	return false, nil
}

// packageSelections returns the selections in the source code of pkg, by the
// position of the selected name. It returns nil when pkg cannot be
// type-checked.
func (c *methodCheck) packageSelections(ctx context.Context, pkg string) map[sourcePosition]*types.Selection {
	if selections, ok := c.selections[pkg]; ok {
		return selections
	}
	c.sp.Info("type-checking a package for the import cycle check of a method_call rule", "package", pkg)
	selections, err := c.loadSelections(ctx, pkg)
	if err != nil {
		// The check only improves the error of a build that would fail
		// later, so it must not fail a build on its own.
		c.sp.Warn("skipping the import cycle check of method_call rules", "package", pkg, "error", err)
	}
	c.selections[pkg] = selections
	return selections
}

func (c *methodCheck) loadSelections(ctx context.Context, pkg string) (map[sourcePosition]*types.Selection, error) {
	ctx, cancel := context.WithTimeout(ctx, packagesLoadTimeout)
	defer cancel()
	pkgs, err := pkgload.LoadPackages(ctx,
		packages.NeedName|packages.NeedSyntax|packages.NeedTypes|packages.NeedTypesInfo,
		c.sp.buildFlags, c.sp.tests, pkg)
	if err != nil {
		return nil, err
	}
	selections := make(map[sourcePosition]*types.Selection)
	// A test build has a variant of the package with its _test.go files.
	for _, p := range pkgs {
		if p.PkgPath != pkg || p.TypesInfo == nil {
			continue
		}
		for sel, selection := range p.TypesInfo.Selections {
			pos := p.Fset.Position(sel.Sel.Pos())
			selections[sourcePosition{file: filepath.Base(pos.Filename), line: pos.Line, col: pos.Column}] = selection
		}
	}
	return selections, nil
}

// reachable reports whether from imports to, directly or indirectly, through
// the imports in graph and the rule imports in added. memo keeps, for each
// package, the packages that it imports, and must only be used with the same
// graph and added.
func reachable(
	memo map[string]map[string]bool,
	graph map[string][]string,
	added map[string][]*ruleImport,
	from, to string,
) bool {
	seen, ok := memo[from]
	if !ok {
		seen = make(map[string]bool)
		stack := []string{from}
		visit := func(pkg string) {
			if !seen[pkg] {
				seen[pkg] = true
				stack = append(stack, pkg)
			}
		}
		for len(stack) > 0 {
			pkg := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			for _, imp := range graph[pkg] {
				visit(imp)
			}
			for _, ri := range added[pkg] {
				visit(ri.path)
			}
		}
		memo[from] = seen
	}
	return seen[to]
}
