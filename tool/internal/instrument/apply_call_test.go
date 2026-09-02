// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"go/token"
	"strings"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
)

// makeCallFile builds a minimal *dst.File containing a single function whose
// body consists of a single expression statement holding the given call.
func makeCallFile(call *dst.CallExpr) *dst.File {
	return &dst.File{
		Name: &dst.Ident{Name: "main"},
		Decls: []dst.Decl{
			&dst.FuncDecl{
				Name: &dst.Ident{Name: "f"},
				Type: &dst.FuncType{Params: &dst.FieldList{}},
				Body: &dst.BlockStmt{
					List: []dst.Stmt{
						&dst.ExprStmt{X: call},
					},
				},
			},
		},
	}
}

func httpGetCall() *dst.CallExpr {
	return &dst.CallExpr{
		Fun: &dst.SelectorExpr{
			X:   &dst.Ident{Name: "http", Path: "net/http"},
			Sel: &dst.Ident{Name: "Get"},
		},
		Args: []dst.Expr{&dst.BasicLit{Kind: token.STRING, Value: `"url"`}},
	}
}

func httpGetRule(replace string) *rule.InstCallRule {
	return &rule.InstCallRule{
		InstBaseRule: rule.InstBaseRule{Name: "wrap_get"},
		FunctionCall: "net/http.Get",
		ImportPath:   "net/http",
		FuncName:     "Get",
		Replace:      replace,
	}
}

func TestWalkCallsWithEnclosingFunc_VisitsAllCallsWithEnclosingFunc(t *testing.T) {
	root := parseFile(t, `package main

import "fmt"

var result = fmt.Sprintf("x")

func A() {
	fmt.Println("a")
}

func B() {
	fmt.Println("b1")
	fmt.Println("b2")
}
`)

	var enclosingNames []string
	walkCallsWithEnclosingFunc(root, func(_ *dst.CallExpr, enclosing *dst.FuncDecl) bool {
		name := "<none>"
		if enclosing != nil {
			name = enclosing.Name.Name
		}
		enclosingNames = append(enclosingNames, name)
		return true
	})

	assert.Equal(t, []string{"<none>", "A", "B", "B"}, enclosingNames)
}

func TestWalkCallsWithEnclosingFunc_StopsWithinDecl(t *testing.T) {
	root := parseFile(t, `package main

import "fmt"

func A() {
	fmt.Println("first")
	fmt.Println("second")
}
`)

	visited := 0
	walkCallsWithEnclosingFunc(root, func(_ *dst.CallExpr, _ *dst.FuncDecl) bool {
		visited++
		return false
	})

	assert.Equal(t, 1, visited, "must stop inspecting further calls within the same decl once fn returns false")
}

func TestWalkCallsWithEnclosingFunc_StopsAcrossDecls(t *testing.T) {
	root := parseFile(t, `package main

import "fmt"

func A() {
	fmt.Println("a")
}

func B() {
	fmt.Println("b")
}
`)

	var visited []string
	walkCallsWithEnclosingFunc(root, func(_ *dst.CallExpr, enclosing *dst.FuncDecl) bool {
		visited = append(visited, enclosing.Name.Name)
		return false
	})

	assert.Equal(t, []string{"A"}, visited)
}

// --- applyCallRule tests ---

func TestApplyCallRule_Success(t *testing.T) {
	file := makeCallFile(httpGetCall())
	r := httpGetRule("traced({{ . }})")

	err := newTestPhase().applyCallRule(context.Background(), r, file)

	require.NoError(t, err)
	stmt := file.Decls[0].(*dst.FuncDecl).Body.List[0].(*dst.ExprStmt)
	outerCall, ok := stmt.X.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr after wrap, got %T", stmt.X)
	fn, ok := outerCall.Fun.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "traced", fn.Name)
	require.Len(t, outerCall.Args, 1)
	_, ok = outerCall.Args[0].(*dst.CallExpr)
	require.True(t, ok, "expected inner argument to be a call expression")
}

func TestApplyCallRule_NonCallExprResult(t *testing.T) {
	// Replace produces a selector expression, not a call expression.
	file := makeCallFile(httpGetCall())
	r := httpGetRule("{{ . }}.Response")

	err := newTestPhase().applyCallRule(context.Background(), r, file)

	require.NoError(t, err)
	stmt := file.Decls[0].(*dst.FuncDecl).Body.List[0].(*dst.ExprStmt)
	_, ok := stmt.X.(*dst.SelectorExpr)
	require.True(t, ok, "expected *dst.SelectorExpr after wrap, got %T", stmt.X)
}

func TestApplyCallRule_InvalidTemplate(t *testing.T) {
	// An unclosed template tag fails text/template parsing in newCallTemplate.
	file := makeCallFile(httpGetCall())
	r := httpGetRule("wrapper({{")

	err := newTestPhase().applyCallRule(context.Background(), r, file)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse template")
}

func TestApplyCallRule_AppendArgs(t *testing.T) {
	file := makeCallFile(httpGetCall())
	r := httpGetRule("")
	r.AppendArgs = []string{"traced.Context()"}
	r.Imports = map[string]string{"traced": "fmt"}

	err := newTestPhase().applyCallRule(context.Background(), r, file)

	require.NoError(t, err)
	fn := findFuncDeclInFile(t, file, "f")
	stmt := fn.Body.List[0].(*dst.ExprStmt)
	call, ok := stmt.X.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr, got %T", stmt.X)
	require.Len(t, call.Args, 2, "append_args must append onto the matched call")
	assert.True(t, fileImportsPath(file, "fmt"), "import must be added for the append_args-only match")
}

func TestApplyCallRule_AppendArgsWithoutMatch(t *testing.T) {
	// No matching call site: applyCallRule must no-op, including skipping
	// import injection, even though Imports is set on the rule.
	file := makeCallFile(&dst.CallExpr{
		Fun: &dst.SelectorExpr{
			X:   &dst.Ident{Name: "fmt", Path: "fmt"},
			Sel: &dst.Ident{Name: "Println"},
		},
		Args: []dst.Expr{&dst.BasicLit{Kind: token.STRING, Value: `"hello"`}},
	})
	r := httpGetRule("")
	r.AppendArgs = []string{"traced.Context()"}
	r.Imports = map[string]string{"traced": "example.com/traced"}

	err := newTestPhase().applyCallRule(context.Background(), r, file)

	require.NoError(t, err)
	assert.False(t, fileImportsPath(file, "example.com/traced"))
}

func TestApplyCallRule_ImportAliasMismatch(t *testing.T) {
	root := parseFile(t, `package main

import (
	f "fmt"
	"net/http"
)

func Run() {
	http.Get("url")
}
`)
	r := httpGetRule("traced.Call({{ . }})")
	r.Imports = map[string]string{"traced": "fmt"}

	err := newTestPhase().applyCallRule(context.Background(), r, root)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "import alias mismatch")
}

func TestApplyCallRule_FuncArgumentUsesEnclosingFunction(t *testing.T) {
	root := parseFile(t, `package main

import "net/http"

func Handler(name string) {
	http.Get("url")
}
`)
	r := httpGetRule("traced({{ .FuncArgument 0 }}, {{ . }})")

	err := newTestPhase().applyCallRule(context.Background(), r, root)

	require.NoError(t, err)
	handler := findFuncDeclInFile(t, root, "Handler")
	stmt := handler.Body.List[0].(*dst.ExprStmt)
	outerCall, ok := stmt.X.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr after wrap, got %T", stmt.X)
	require.Len(t, outerCall.Args, 2)
	nameArg, ok := outerCall.Args[0].(*dst.Ident)
	require.True(t, ok, "expected *dst.Ident, got %T", outerCall.Args[0])
	assert.Equal(t, "name", nameArg.Name)
}

func TestApplyCallRule_ConditionalReplace(t *testing.T) {
	replace := `{{- $ctx := .FuncArgumentOfType "context.Context" -}}
{{- if $ctx -}}
traced.Call({{ $ctx }}, {{ .CallArgument 0 }})
{{- else -}}
{{ . }}
{{- end -}}`

	newRule := func() *rule.InstCallRule {
		r := httpGetRule(replace)
		r.Imports = map[string]string{"traced": "fmt"}
		return r
	}

	t.Run("context.Context argument present builds a new call", func(t *testing.T) {
		root := parseFile(t, `package main

import "net/http"

func Run(ctx context.Context) {
	http.Get("url")
}
`)
		r := httpGetRule(replace)

		err := newTestPhase().applyCallRule(context.Background(), r, root)
		require.NoError(t, err)

		fn := findFuncDeclInFile(t, root, "Run")
		stmt := fn.Body.List[0].(*dst.ExprStmt)
		call, ok := stmt.X.(*dst.CallExpr)
		require.True(t, ok, "expected *dst.CallExpr, got %T", stmt.X)

		sel, ok := call.Fun.(*dst.SelectorExpr)
		require.True(t, ok, "expected *dst.SelectorExpr, got %T", call.Fun)
		assert.Equal(t, "traced", sel.X.(*dst.Ident).Name)
		assert.Equal(t, "Call", sel.Sel.Name)
		require.Len(t, call.Args, 2)

		ctxArg, ok := call.Args[0].(*dst.Ident)
		require.True(t, ok, "expected *dst.Ident, got %T", call.Args[0])
		assert.Equal(t, "ctx", ctxArg.Name)
	})

	t.Run("no context.Context argument wraps the original call", func(t *testing.T) {
		root := parseFile(t, `package main

import "net/http"

func Run(name string) {
	http.Get("url")
}
`)
		r := httpGetRule(replace)

		err := newTestPhase().applyCallRule(context.Background(), r, root)
		require.NoError(t, err)

		fn := findFuncDeclInFile(t, root, "Run")
		stmt := fn.Body.List[0].(*dst.ExprStmt)
		call, ok := stmt.X.(*dst.CallExpr)
		require.True(t, ok, "expected *dst.CallExpr, got %T", stmt.X)

		sel, ok := call.Fun.(*dst.SelectorExpr)
		require.True(t, ok, "expected *dst.SelectorExpr, got %T", call.Fun)
		assert.Equal(t, "http", sel.X.(*dst.Ident).Name)
		assert.Equal(t, "Get", sel.Sel.Name)
		require.Len(t, call.Args, 1)
	})

	t.Run("correctly add the import", func(t *testing.T) {
		root := parseFile(t, `package main

import "net/http"

func Run(ctx context.Context) {
	http.Get("url")
}
`)
		err := newTestPhase().applyCallRule(context.Background(), newRule(), root)

		require.NoError(t, err)
		assert.True(t, fileImportsPath(root, "fmt"), "import must be added when the taken branch references it")
	})

	t.Run("do not add the import", func(t *testing.T) {
		root := parseFile(t, `package main

import "net/http"

func Run(name string) {
	http.Get("url")
}
`)
		err := newTestPhase().applyCallRule(context.Background(), newRule(), root)

		require.NoError(t, err)
		assert.False(
			t,
			fileImportsPath(root, "fmt"),
			"import must not be added when no matched call site references it",
		)
	})

	t.Run("multiple call sites", func(t *testing.T) {
		root := parseFile(t, `package main

import "net/http"

func WithContext(ctx context.Context) {
	http.Get("url")
}

func WithoutContext(name string) {
	http.Get("url")
}
`)
		err := newTestPhase().applyCallRule(context.Background(), newRule(), root)

		require.NoError(t, err)
		assert.True(
			t,
			fileImportsPath(root, "fmt"),
			"import must be kept file-wide when any matched call site needs it",
		)
	})
}

// fileImportsPath reports whether root has a top-level import declaration
// for the given import path.
func fileImportsPath(root *dst.File, path string) bool {
	for _, decl := range root.Decls {
		genDecl, ok := decl.(*dst.GenDecl)
		if !ok || genDecl.Tok != token.IMPORT {
			continue
		}
		for _, spec := range genDecl.Specs {
			importSpec, specOk := spec.(*dst.ImportSpec)
			if specOk && strings.Trim(importSpec.Path.Value, `"`) == path {
				return true
			}
		}
	}
	return false
}

func TestUsedRuleImports_BlankAndDotAliasesAlwaysKept(t *testing.T) {
	root := parseFile(t, `package main

func f() {}
`)
	ruleImports := map[string]string{
		"_": "example.com/sideeffect",
		".": "example.com/dotimport",
	}

	used := usedRuleImports(root, ruleImports)

	assert.Equal(t, ruleImports, used)
}

func TestUsedRuleImports_OnlyReferencedAliasesKept(t *testing.T) {
	root := parseFile(t, `package main

func f() {
	traced.Call()
}
`)
	ruleImports := map[string]string{
		"traced":    "fmt",
		"unrelated": "example.com/unrelated",
	}

	used := usedRuleImports(root, ruleImports)

	assert.Equal(t, map[string]string{"traced": "fmt"}, used)
}

func TestUsedRuleImports_EmptyRuleImports(t *testing.T) {
	root := parseFile(t, `package main

func f() {}
`)

	used := usedRuleImports(root, nil)

	assert.Nil(t, used)
}

func TestUsedRuleImports_PlainIdentifierWithoutSelector(t *testing.T) {
	root := parseFile(t, `package main

func f() {
	use(traced)
}
`)
	ruleImports := map[string]string{"traced": "fmt"}

	used := usedRuleImports(root, ruleImports)

	assert.Empty(t, used)
}

func TestUsedRuleImports_ChainedSelector(t *testing.T) {
	root := parseFile(t, `package main

func f() {
	pkg.traced.Call()
}
`)
	ruleImports := map[string]string{"traced": "fmt"}

	used := usedRuleImports(root, ruleImports)

	assert.Empty(t, used)
}

func TestUsedRuleImports_MultipleReferencesCountedOnce(t *testing.T) {
	root := parseFile(t, `package main

func f() {
	traced.Call()
	traced.Call()
}
`)
	ruleImports := map[string]string{"traced": "fmt"}

	used := usedRuleImports(root, ruleImports)

	assert.Equal(t, map[string]string{"traced": "fmt"}, used)
}

func TestUsedRuleImports_MixedAliasKinds(t *testing.T) {
	root := parseFile(t, `package main

func f() {
	traced.Call()
}
`)
	ruleImports := map[string]string{
		"traced": "fmt",
		"unused": "example.com/unused",
		"_":      "example.com/sideeffect",
		".":      "example.com/dotimport",
	}

	used := usedRuleImports(root, ruleImports)

	assert.Equal(t, map[string]string{
		"traced": "fmt",
		"_":      "example.com/sideeffect",
		".":      "example.com/dotimport",
	}, used)
}

func TestApplyCallRule_FuncTagWithoutEnclosingFunctionErrors(t *testing.T) {
	root := parseFile(t, `package main

import "net/http"

var resp, _ = http.Get("url")
`)
	r := httpGetRule("traced({{ .FuncName }}, {{ . }})")

	err := newTestPhase().applyCallRule(context.Background(), r, root)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no enclosing function is available")
}

// findFuncDeclInFile returns the top-level function declaration named name.
func findFuncDeclInFile(t *testing.T, root *dst.File, name string) *dst.FuncDecl {
	t.Helper()
	for _, decl := range root.Decls {
		if fn, ok := decl.(*dst.FuncDecl); ok && fn.Name.Name == name {
			return fn
		}
	}
	require.Fail(t, "function not found", "name: %s", name)
	return nil
}

// parseFile parses source into a *dst.File.
func parseFile(t *testing.T, source string) *dst.File {
	t.Helper()
	parser := ast.NewAstParser()
	root, err := parser.ParseSource(source)
	require.NoError(t, err)
	return root
}

// --- matchesCallRule tests ---

func TestMatchesCallRule_QualifiedCallMatches(t *testing.T) {
	r := &rule.InstCallRule{
		ImportPath: "net/http",
		FuncName:   "Get",
	}

	call := &dst.CallExpr{
		Fun: &dst.SelectorExpr{
			X: &dst.Ident{
				Name: "http",
				Path: "net/http",
			},
			Sel: &dst.Ident{Name: "Get"},
		},
	}

	matches := matchesCallRule(call, r, nil)

	assert.True(t, matches)
}

func TestMatchesCallRule_UnqualifiedCallDoesNotMatch(t *testing.T) {
	r := &rule.InstCallRule{
		ImportPath: "net/http",
		FuncName:   "Get",
	}

	// Unqualified call: Get() instead of http.Get()
	call := &dst.CallExpr{
		Fun: &dst.Ident{Name: "Get"},
	}

	matches := matchesCallRule(call, r, nil)

	assert.False(t, matches)
}

func TestMatchesCallRule_WrongPackage(t *testing.T) {
	r := &rule.InstCallRule{
		ImportPath: "net/http",
		FuncName:   "Get",
	}

	call := &dst.CallExpr{
		Fun: &dst.SelectorExpr{
			X: &dst.Ident{
				Name: "other",
				Path: "other/package",
			},
			Sel: &dst.Ident{Name: "Get"},
		},
	}

	matches := matchesCallRule(call, r, nil)

	assert.False(t, matches)
}

func TestMatchesCallRule_WrongFunctionName(t *testing.T) {
	r := &rule.InstCallRule{
		ImportPath: "net/http",
		FuncName:   "Get",
	}

	call := &dst.CallExpr{
		Fun: &dst.SelectorExpr{
			X: &dst.Ident{
				Name: "http",
				Path: "net/http",
			},
			Sel: &dst.Ident{Name: "Post"}, // Wrong function
		},
	}

	matches := matchesCallRule(call, r, nil)

	assert.False(t, matches)
}

func TestMatchesCallRule_NonSelectorExpression(t *testing.T) {
	r := &rule.InstCallRule{
		ImportPath: "net/http",
		FuncName:   "Get",
	}

	// Call with non-selector function (e.g., function literal)
	call := &dst.CallExpr{
		Fun: &dst.FuncLit{},
	}

	matches := matchesCallRule(call, r, nil)

	assert.False(t, matches)
}

func TestMatchesCallRule_ImportAliasFromVersionSuffix(t *testing.T) {
	r := &rule.InstCallRule{
		ImportPath: "example.com/foo/v2",
		FuncName:   "Bar",
	}

	call := &dst.CallExpr{
		Fun: &dst.SelectorExpr{
			X:   &dst.Ident{Name: "foo"},
			Sel: &dst.Ident{Name: "Bar"},
		},
	}

	file := &dst.File{
		Decls: []dst.Decl{
			&dst.GenDecl{
				Tok: token.IMPORT,
				Specs: []dst.Spec{
					&dst.ImportSpec{
						Path: &dst.BasicLit{Value: `"example.com/foo/v2"`},
					},
				},
			},
		},
	}

	importAliases := ast.ImportAliasMap(file)
	matches := matchesCallRule(call, r, importAliases)

	assert.True(t, matches)
}

func TestAppendCallArgs_Empty(t *testing.T) {
	r := &rule.InstCallRule{}
	call := &dst.CallExpr{Fun: &dst.Ident{Name: "f"}}

	modified, err := appendCallArgs(call, r)

	require.NoError(t, err)
	assert.False(t, modified)
	assert.Empty(t, call.Args)
}

func TestAppendCallArgs_SimpleAppend(t *testing.T) {
	r := &rule.InstCallRule{
		AppendArgs: []string{"42", "true"},
	}
	call := &dst.CallExpr{
		Fun:  &dst.Ident{Name: "f"},
		Args: []dst.Expr{&dst.Ident{Name: "a"}},
	}

	modified, err := appendCallArgs(call, r)

	require.NoError(t, err)
	assert.True(t, modified)
	assert.Len(t, call.Args, 3)
}

func TestAppendCallArgs_EllipsisNoVariadicType(t *testing.T) {
	r := &rule.InstCallRule{
		AppendArgs: []string{"42"},
	}
	call := &dst.CallExpr{
		Fun:      &dst.Ident{Name: "f"},
		Args:     []dst.Expr{&dst.Ident{Name: "opts"}},
		Ellipsis: true,
	}

	modified, err := appendCallArgs(call, r)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "variadic_type")
	assert.False(t, modified)
}

func TestAppendCallArgs_EllipsisWithVariadicType(t *testing.T) {
	r := &rule.InstCallRule{
		AppendArgs:   []string{"42"},
		VariadicType: "int",
	}
	call := &dst.CallExpr{
		Fun:      &dst.Ident{Name: "f"},
		Args:     []dst.Expr{&dst.Ident{Name: "opts"}},
		Ellipsis: true,
	}

	modified, err := appendCallArgs(call, r)

	require.NoError(t, err)
	assert.True(t, modified)
	// The outer call still has Ellipsis=true
	assert.True(t, call.Ellipsis)
	// The last arg is now an IIFE call
	require.Len(t, call.Args, 1)
	iifeCall, ok := call.Args[0].(*dst.CallExpr)
	require.True(t, ok, "expected IIFE call expression")
	// The IIFE's function is a FuncLit
	_, ok = iifeCall.Fun.(*dst.FuncLit)
	assert.True(t, ok, "expected FuncLit as IIFE function")
}

func TestAppendCallArgs_EllipsisNoArgs(t *testing.T) {
	r := &rule.InstCallRule{
		AppendArgs:   []string{"42"},
		VariadicType: "int",
	}
	call := &dst.CallExpr{
		Fun:      &dst.Ident{Name: "f"},
		Args:     []dst.Expr{},
		Ellipsis: true,
	}

	modified, err := appendCallArgs(call, r)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no arguments")
	assert.False(t, modified)
}

func TestAppendCallArgs_InvalidVariadicType(t *testing.T) {
	r := &rule.InstCallRule{
		AppendArgs:   []string{"42"},
		VariadicType: "func {{{",
	}
	call := &dst.CallExpr{
		Fun:      &dst.Ident{Name: "f"},
		Args:     []dst.Expr{&dst.Ident{Name: "opts"}},
		Ellipsis: true,
	}

	modified, err := appendCallArgs(call, r)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse variadic_type")
	assert.False(t, modified)
}

func TestAppendCallArgs_InvalidExpr(t *testing.T) {
	r := &rule.InstCallRule{
		AppendArgs: []string{"func {{{"},
	}
	call := &dst.CallExpr{Fun: &dst.Ident{Name: "f"}}

	modified, err := appendCallArgs(call, r)

	require.Error(t, err)
	assert.False(t, modified)
}

func TestAppendCallArgs_WithReplace(t *testing.T) {
	// Both append_args and replace: args appended first, then replace wraps.
	call := httpGetCall()
	file := makeCallFile(call)
	r := &rule.InstCallRule{
		InstBaseRule: rule.InstBaseRule{Name: "wrap_get"},
		FunctionCall: "net/http.Get",
		ImportPath:   "net/http",
		FuncName:     "Get",
		AppendArgs:   []string{"42"},
		Replace:      "wrapper({{ . }})",
	}

	err := newTestPhase().applyCallRule(context.Background(), r, file)
	require.NoError(t, err)

	stmt := file.Decls[0].(*dst.FuncDecl).Body.List[0].(*dst.ExprStmt)
	outerCall, ok := stmt.X.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr after wrap, got %T", stmt.X)
	// Outer call is "wrapper"
	wrapperIdent, ok := outerCall.Fun.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "wrapper", wrapperIdent.Name)
	// Inner call has 2 args (original + appended 42)
	require.Len(t, outerCall.Args, 1)
	innerCall, ok := outerCall.Args[0].(*dst.CallExpr)
	require.True(t, ok)
	assert.Len(t, innerCall.Args, 2)
}

func TestBuildEllipsisIIFE_Structure(t *testing.T) {
	varType := &dst.Ident{Name: "int"}
	spreadArg := &dst.Ident{Name: "opts"}
	newArgs := []dst.Expr{&dst.BasicLit{Value: "42"}}

	iife := buildEllipsisIIFE(spreadArg, varType, newArgs)

	// Outer call: funcLit(opts...)
	assert.True(t, iife.Ellipsis)
	require.Len(t, iife.Args, 1)
	assert.Equal(t, spreadArg, iife.Args[0])

	funcLit, ok := iife.Fun.(*dst.FuncLit)
	require.True(t, ok)

	// Param: v ...int
	require.Len(t, funcLit.Type.Params.List, 1)
	param := funcLit.Type.Params.List[0]
	assert.Equal(t, "v", param.Names[0].Name)
	_, ok = param.Type.(*dst.Ellipsis)
	assert.True(t, ok)

	// Return: []int
	require.Len(t, funcLit.Type.Results.List, 1)
	retType, ok := funcLit.Type.Results.List[0].Type.(*dst.ArrayType)
	require.True(t, ok)
	assert.Equal(t, "int", retType.Elt.(*dst.Ident).Name)

	// Body: return append(v, 42)
	require.Len(t, funcLit.Body.List, 1)
	retStmt, ok := funcLit.Body.List[0].(*dst.ReturnStmt)
	require.True(t, ok)
	require.Len(t, retStmt.Results, 1)
	appendCall, ok := retStmt.Results[0].(*dst.CallExpr)
	require.True(t, ok)
	assert.Equal(t, "append", appendCall.Fun.(*dst.Ident).Name)
	assert.Len(t, appendCall.Args, 2)
}

func TestMatchesCallRule_ImportAliasFromGopkgIn(t *testing.T) {
	r := &rule.InstCallRule{
		ImportPath: "gopkg.in/yaml.v3",
		FuncName:   "Unmarshal",
	}

	call := &dst.CallExpr{
		Fun: &dst.SelectorExpr{
			X:   &dst.Ident{Name: "yaml"},
			Sel: &dst.Ident{Name: "Unmarshal"},
		},
	}

	file := &dst.File{
		Decls: []dst.Decl{
			&dst.GenDecl{
				Tok: token.IMPORT,
				Specs: []dst.Spec{
					&dst.ImportSpec{
						Path: &dst.BasicLit{Value: `"gopkg.in/yaml.v3"`},
					},
				},
			},
		},
	}

	importAliases := ast.ImportAliasMap(file)
	matches := matchesCallRule(call, r, importAliases)

	assert.True(t, matches)
}

func TestApplyCallRule_NoMatchIsNoOp(t *testing.T) {
	file := makeCallFile(&dst.CallExpr{
		Fun: &dst.SelectorExpr{
			X:   &dst.Ident{Name: "fmt", Path: "fmt"},
			Sel: &dst.Ident{Name: "Println"},
		},
		Args: []dst.Expr{&dst.BasicLit{Kind: token.STRING, Value: `"hello"`}},
	})

	r := &rule.InstCallRule{
		InstBaseRule: rule.InstBaseRule{Name: "wrap_sizeof"},
		FunctionCall: "unsafe.Sizeof",
		ImportPath:   "unsafe",
		FuncName:     "Sizeof",
		Replace:      "Wrapper({{ . }})",
	}

	err := newTestPhase().applyCallRule(context.Background(), r, file)

	require.NoError(t, err, "applyCallRule must no-op when no calls match")
}

func TestApplyCallAppendArgs_NoMatchReturnsFalse(t *testing.T) {
	// A file with no matching calls should cause applyCallAppendArgs to
	// return false so applyCallRule can skip the file as a no-op.
	file := makeCallFile(&dst.CallExpr{
		Fun: &dst.SelectorExpr{
			X:   &dst.Ident{Name: "fmt", Path: "fmt"},
			Sel: &dst.Ident{Name: "Println"},
		},
		Args: []dst.Expr{&dst.BasicLit{Kind: token.STRING, Value: `"hello"`}},
	})

	r := &rule.InstCallRule{
		InstBaseRule: rule.InstBaseRule{Name: "no_match"},
		FunctionCall: "net/http.Get",
		ImportPath:   "net/http",
		FuncName:     "Get",
		AppendArgs:   []string{"ctx"},
	}

	ip := newTestPhase()
	importAliases := ast.ImportAliasMap(file)
	result := ip.applyCallAppendArgs(r, file, importAliases)

	assert.False(t, result, "applyCallAppendArgs must return false when no calls match")
}

func TestApplyCallRule_WrapFailureReturnsError(t *testing.T) {
	// Template parses but generates invalid Go when applied to the matched call.
	file := makeCallFile(httpGetCall())
	r := httpGetRule("not a valid expression {{ . }}")

	err := newTestPhase().applyCallRule(context.Background(), r, file)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse generated code")
}
