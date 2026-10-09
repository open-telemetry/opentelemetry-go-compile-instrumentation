// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"bytes"
	"context"
	goast "go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/gcexportdata"

	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/imports"
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

	modified, err := newTestPhase().applyCallRule(context.Background(), r, file)

	require.NoError(t, err)
	require.True(t, modified, "the rule should have changed the file")
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

	modified, err := newTestPhase().applyCallRule(context.Background(), r, file)

	require.NoError(t, err)
	require.True(t, modified, "the rule should have changed the file")
	stmt := file.Decls[0].(*dst.FuncDecl).Body.List[0].(*dst.ExprStmt)
	_, ok := stmt.X.(*dst.SelectorExpr)
	require.True(t, ok, "expected *dst.SelectorExpr after wrap, got %T", stmt.X)
}

func TestApplyCallRule_InvalidTemplate(t *testing.T) {
	// An unclosed template tag fails text/template parsing in newCallTemplate.
	file := makeCallFile(httpGetCall())
	r := httpGetRule("wrapper({{")

	modified, err := newTestPhase().applyCallRule(context.Background(), r, file)

	require.Error(t, err)
	require.False(t, modified, "a rule that matched nothing or failed must not report a change")
	assert.Contains(t, err.Error(), "failed to parse template")
}

func TestApplyCallRule_AppendArgs(t *testing.T) {
	file := makeCallFile(httpGetCall())
	r := httpGetRule("")
	r.AppendArgs = []string{"traced.Context()"}
	r.Imports = map[string]string{"traced": "fmt"}

	modified, err := newTestPhase().applyCallRule(context.Background(), r, file)

	require.NoError(t, err)
	require.True(t, modified, "the rule should have changed the file")
	fn := findFuncDeclInFile(t, file, "f")
	stmt := fn.Body.List[0].(*dst.ExprStmt)
	call, ok := stmt.X.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr, got %T", stmt.X)
	require.Len(t, call.Args, 2, "append_args must append onto the matched call")
	assert.True(t, fileImportsPath(file, "fmt"), "import must be added for the append_args-only match")
}

func TestApplyCallRule_InvalidAppendArgs(t *testing.T) {
	file := makeCallFile(httpGetCall())
	r := httpGetRule("")
	r.AppendArgs = []string{"func("}

	_, err := newTestPhase().applyCallRule(context.Background(), r, file)

	require.Error(t, err)
	assert.Contains(t, err.Error(), `append_args entry "func("`)
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

	modified, err := newTestPhase().applyCallRule(context.Background(), r, file)

	require.NoError(t, err)
	require.False(t, modified, "a rule that matched nothing or failed must not report a change")
	assert.False(t, fileImportsPath(file, "example.com/traced"))
}

func TestApplyCallRule_ReplaceHonorsIgnoreDirective(t *testing.T) {
	root := parseFile(t, `package main

import "net/http"

func Run() {
	//otelc:ignore
	http.Get("ignored")
	http.Get("kept")
}
`)
	r := httpGetRule("traced({{ . }})")

	_, err := newTestPhase().applyCallRule(context.Background(), r, root)

	require.NoError(t, err)
	fn := findFuncDeclInFile(t, root, "Run")
	ignoredStmt := fn.Body.List[0].(*dst.ExprStmt)
	_, ignoredStillBare := ignoredStmt.X.(*dst.CallExpr)
	assert.True(t, ignoredStillBare, "annotated call site must keep its original form")

	keptStmt := fn.Body.List[1].(*dst.ExprStmt)
	keptCall, ok := keptStmt.X.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr after wrap, got %T", keptStmt.X)
	fnIdent, ok := keptCall.Fun.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "traced", fnIdent.Name, "the other call site to the same function must stay instrumented")
}

func TestApplyCallRule_AppendArgsHonorsIgnoreDirective(t *testing.T) {
	root := parseFile(t, `package main

import "net/http"

func Run() {
	//otelc:ignore
	http.Get("ignored")
	http.Get("kept")
}
`)
	r := httpGetRule("")
	r.AppendArgs = []string{"traced.Context()"}
	r.Imports = map[string]string{"traced": "fmt"}

	_, err := newTestPhase().applyCallRule(context.Background(), r, root)

	require.NoError(t, err)
	fn := findFuncDeclInFile(t, root, "Run")
	ignoredCall := fn.Body.List[0].(*dst.ExprStmt).X.(*dst.CallExpr)
	assert.Len(t, ignoredCall.Args, 1, "annotated call site must not gain the appended argument")

	keptCall := fn.Body.List[1].(*dst.ExprStmt).X.(*dst.CallExpr)
	assert.Len(t, keptCall.Args, 2, "the other call site to the same function must gain the appended argument")
}

func TestApplyCallRule_AppendArgsFailureIsUnmodified(t *testing.T) {
	// The call matches, but append_args on an ellipsis call needs
	// variadic_type, so the rule fails and leaves the call unchanged.
	call := httpGetCall()
	call.Ellipsis = true
	file := makeCallFile(call)
	r := httpGetRule("")
	r.AppendArgs = []string{"traced.Context()"}
	r.Imports = map[string]string{"traced": "example.com/traced"}

	modified, err := newTestPhase().applyCallRule(context.Background(), r, file)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "variadic_type")
	require.False(t, modified, "a failed rule must not report a change")
	assert.Len(t, call.Args, 1)
	assert.False(t, fileImportsPath(file, "example.com/traced"))
}

func TestApplyCallRule_ImportAliasMismatchUsesFileExistingAlias(t *testing.T) {
	// The rule is written against the alias "traced" for "fmt". The
	// file already imports "fmt" under its own alias "f". The injected
	// code must use "f", not fail the build.
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

	_, err := newTestPhase().applyCallRule(context.Background(), r, root)

	require.NoError(t, err)
	run := findFuncDeclInFile(t, root, "Run")
	stmt := run.Body.List[0].(*dst.ExprStmt)
	call, ok := stmt.X.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr after wrap, got %T", stmt.X)
	sel, ok := call.Fun.(*dst.SelectorExpr)
	require.True(t, ok, "expected *dst.SelectorExpr, got %T", call.Fun)
	ident, ok := sel.X.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "f", ident.Name, "injected code must use the file's existing alias, not the rule's")
	assert.Equal(t, "Call", sel.Sel.Name)
}

func TestApplyCallRule_UnrelatedSelectorSharingRuleAliasDoesNotBlockOverride(t *testing.T) {
	root := parseFile(t, `package main

import (
	f "fmt"
	"net/http"
)

func Other() {
	traced.Value()
}

func Run() {
	http.Get("url")
}
`)
	r := httpGetRule("traced.Call({{ . }})")
	r.Imports = map[string]string{"traced": "fmt"}

	_, err := newTestPhase().applyCallRule(context.Background(), r, root)

	require.NoError(t, err)
	run := findFuncDeclInFile(t, root, "Run")
	stmt := run.Body.List[0].(*dst.ExprStmt)
	call, ok := stmt.X.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr after wrap, got %T", stmt.X)
	sel, ok := call.Fun.(*dst.SelectorExpr)
	require.True(t, ok, "expected *dst.SelectorExpr, got %T", call.Fun)
	ident, ok := sel.X.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "f", ident.Name, "injected code must use the file's existing alias, not the rule's")
}

func TestApplyCallRule_OverrideShadowedByParameterReportsConflict(t *testing.T) {
	root := parseFile(t, `package main

import (
	f "fmt"
	"net/http"
)

func Run(f sink) {
	http.Get("url")
}
`)
	r := httpGetRule("traced.Call({{ . }})")
	r.Imports = map[string]string{"traced": "fmt"}

	_, err := newTestPhase().applyCallRule(context.Background(), r, root)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "alias override conflict")
	run := findFuncDeclInFile(t, root, "Run")
	stmt, ok := run.Body.List[0].(*dst.ExprStmt)
	require.True(t, ok, "expected *dst.ExprStmt, got %T", run.Body.List[0])
	_, ok = stmt.X.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr, got %T", stmt.X)
	assert.Equal(t, "http", stmt.X.(*dst.CallExpr).Fun.(*dst.SelectorExpr).X.(*dst.Ident).Name,
		"must not wrap the call when the override would resolve to the wrong identifier")
}

func TestApplyCallReplace_DoesNotRewriteOriginalCallArguments(t *testing.T) {
	call := &dst.CallExpr{
		Fun: &dst.SelectorExpr{X: &dst.Ident{Name: "http"}, Sel: &dst.Ident{Name: "Get"}},
		Args: []dst.Expr{
			&dst.CallExpr{Fun: &dst.SelectorExpr{X: &dst.Ident{Name: "traced"}, Sel: &dst.Ident{Name: "Value"}}},
		},
	}
	root := makeCallFile(call)
	r := httpGetRule(`traced.Wrap({{ . }})`)
	importAliases := map[string]string{"http": "net/http"}
	aliasOverrides := map[string]string{"traced": "f"}

	modified, err := newTestPhase().applyCallReplace(r, root, importAliases, aliasOverrides)

	require.NoError(t, err)
	assert.True(t, modified)
	stmt := root.Decls[0].(*dst.FuncDecl).Body.List[0].(*dst.ExprStmt)
	outer, ok := stmt.X.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr, got %T", stmt.X)
	outerSel, ok := outer.Fun.(*dst.SelectorExpr)
	require.True(t, ok, "expected *dst.SelectorExpr, got %T", outer.Fun)
	assert.Equal(t, "f", outerSel.X.(*dst.Ident).Name, "the rule's own qualifier must move to the file's alias")
	assert.Equal(t, "Wrap", outerSel.Sel.Name)
	require.Len(t, outer.Args, 1)
	inner, ok := outer.Args[0].(*dst.CallExpr)
	require.True(t, ok, "expected the substituted call to stay a *dst.CallExpr, got %T", outer.Args[0])
	require.Len(t, inner.Args, 1)
	argCall, ok := inner.Args[0].(*dst.CallExpr)
	require.True(t, ok, "expected original argument to stay a *dst.CallExpr, got %T", inner.Args[0])
	argSel, ok := argCall.Fun.(*dst.SelectorExpr)
	require.True(t, ok, "expected *dst.SelectorExpr, got %T", argCall.Fun)
	assert.Equal(t, "traced", argSel.X.(*dst.Ident).Name,
		"the substituted call's own argument must not be rewritten just because it shares the rule's alias name")
}

func TestApplyCallRule_FuncArgumentUsesEnclosingFunction(t *testing.T) {
	root := parseFile(t, `package main

import "net/http"

func Handler(name string) {
	http.Get("url")
}
`)
	r := httpGetRule("traced({{ .FuncArgument 0 }}, {{ . }})")

	modified, err := newTestPhase().applyCallRule(context.Background(), r, root)

	require.NoError(t, err)
	require.True(t, modified, "the rule should have changed the file")
	handler := findFuncDeclInFile(t, root, "Handler")
	stmt := handler.Body.List[0].(*dst.ExprStmt)
	outerCall, ok := stmt.X.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr after wrap, got %T", stmt.X)
	require.Len(t, outerCall.Args, 2)
	nameArg, ok := outerCall.Args[0].(*dst.Ident)
	require.True(t, ok, "expected *dst.Ident, got %T", outerCall.Args[0])
	assert.Equal(t, "name", nameArg.Name)
}

func TestApplyCallRule_TypeHelpersUseFileImports(t *testing.T) {
	t.Run("aliased import", func(t *testing.T) {
		root := parseFile(t, `package main

import althttp "net/http"

func Handler(r *althttp.Request) (resp *althttp.Request, err error) {
	althttp.Get("url")
	return r, nil
}
`)
		r := httpGetRule(
			"traced(" +
				`{{ .FuncArgumentOfType "*net/http.Request" }}, ` +
				`{{ .FuncReturnOfType "*net/http.Request" }}, ` +
				"{{ . }})",
		)

		modified, err := newTestPhase().applyCallRule(context.Background(), r, root)

		require.NoError(t, err)
		require.True(t, modified, "the rule should have changed the file")
		handler := findFuncDeclInFile(t, root, "Handler")
		outerCall, ok := handler.Body.List[0].(*dst.ExprStmt).X.(*dst.CallExpr)
		require.True(t, ok, "expected *dst.CallExpr after wrap, got %T", handler.Body.List[0].(*dst.ExprStmt).X)
		require.Len(t, outerCall.Args, 3)
		assert.Equal(t, "r", outerCall.Args[0].(*dst.Ident).Name)
		assert.Equal(t, "resp", outerCall.Args[1].(*dst.Ident).Name)
	})

	t.Run("disambiguates shared default package name", func(t *testing.T) {
		root := parseFile(t, `package main

import (
	"fmt"
	htmltemplate "html/template"
	"text/template"
)

func Handler(t *template.Template) (page *htmltemplate.Template, err error) {
	fmt.Println("x")
	return nil, nil
}
`)
		replace := "traced(" +
			`{{ .FuncArgumentOfType "*text/template.Template" }}, ` +
			`"{{ .FuncArgumentOfType "*html/template.Template" }}", ` +
			`{{ .FuncReturnOfType "*html/template.Template" }}, ` +
			`"{{ .FuncReturnOfType "*text/template.Template" }}", ` +
			"{{ . }})"
		r := &rule.InstCallRule{
			InstBaseRule: rule.InstBaseRule{Name: "wrap_println"},
			FunctionCall: "fmt.Println",
			ImportPath:   "fmt",
			FuncName:     "Println",
			Replace:      replace,
		}

		modified, err := newTestPhase().applyCallRule(context.Background(), r, root)

		require.NoError(t, err)
		require.True(t, modified, "the rule should have changed the file")
		handler := findFuncDeclInFile(t, root, "Handler")
		outerCall, ok := handler.Body.List[0].(*dst.ExprStmt).X.(*dst.CallExpr)
		require.True(t, ok, "expected *dst.CallExpr after wrap, got %T", handler.Body.List[0].(*dst.ExprStmt).X)
		require.Len(t, outerCall.Args, 5)
		assert.Equal(t, "t", outerCall.Args[0].(*dst.Ident).Name)
		assert.Equal(t, `""`, outerCall.Args[1].(*dst.BasicLit).Value,
			"text/template.Template must not match *html/template.Template")
		assert.Equal(t, "page", outerCall.Args[2].(*dst.Ident).Name)
		assert.Equal(t, `""`, outerCall.Args[3].(*dst.BasicLit).Value,
			"html/template.Template must not match *text/template.Template")
	})
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

import (
	"context"
	"net/http"
)

func Run(ctx context.Context) {
	http.Get("url")
}
`)
		r := httpGetRule(replace)

		modified, err := newTestPhase().applyCallRule(context.Background(), r, root)
		require.NoError(t, err)
		require.True(t, modified, "the rule should have changed the file")

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

		modified, err := newTestPhase().applyCallRule(context.Background(), r, root)
		require.NoError(t, err)
		require.True(t, modified, "the rule should have changed the file")

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

import (
	"context"
	"net/http"
)

func Run(ctx context.Context) {
	http.Get("url")
}
`)
		modified, err := newTestPhase().applyCallRule(context.Background(), newRule(), root)

		require.NoError(t, err)
		require.True(t, modified, "the rule should have changed the file")
		assert.True(t, fileImportsPath(root, "fmt"), "import must be added when the taken branch references it")
	})

	t.Run("do not add the import", func(t *testing.T) {
		root := parseFile(t, `package main

import "net/http"

func Run(name string) {
	http.Get("url")
}
`)
		modified, err := newTestPhase().applyCallRule(context.Background(), newRule(), root)

		require.NoError(t, err)
		require.True(t, modified, "the rule should have changed the file")
		assert.False(
			t,
			fileImportsPath(root, "fmt"),
			"import must not be added when no matched call site references it",
		)
	})

	t.Run("multiple call sites", func(t *testing.T) {
		root := parseFile(t, `package main

import (
	"context"
	"net/http"
)

func WithContext(ctx context.Context) {
	http.Get("url")
}

func WithoutContext(name string) {
	http.Get("url")
}
`)
		modified, err := newTestPhase().applyCallRule(context.Background(), newRule(), root)

		require.NoError(t, err)
		require.True(t, modified, "the rule should have changed the file")
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

func TestApplyCallRule_FuncTagWithoutEnclosingFunctionErrors(t *testing.T) {
	root := parseFile(t, `package main

import "net/http"

var resp, _ = http.Get("url")
`)
	r := httpGetRule("traced({{ .FuncName }}, {{ . }})")

	modified, err := newTestPhase().applyCallRule(context.Background(), r, root)

	require.Error(t, err)
	require.False(t, modified, "a rule that matched nothing or failed must not report a change")
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

func TestMatchesCallRule_ChainedSelectorDoesNotMatch(t *testing.T) {
	r := &rule.InstCallRule{
		ImportPath: "net/http",
		FuncName:   "Get",
	}

	// a.b.Get(): sel.X is itself a selector expression, not a plain
	// identifier, so the package qualifier can't be resolved.
	call := &dst.CallExpr{
		Fun: &dst.SelectorExpr{
			X: &dst.SelectorExpr{
				X:   &dst.Ident{Name: "a"},
				Sel: &dst.Ident{Name: "b"},
			},
			Sel: &dst.Ident{Name: "Get"},
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

	importAliases := ast.ImportAliasMap(file, nil)
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

	modified, err := newTestPhase().applyCallRule(context.Background(), r, file)
	require.NoError(t, err)
	require.True(t, modified, "the rule should have changed the file")

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

	importAliases := ast.ImportAliasMap(file, nil)
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

	modified, err := newTestPhase().applyCallRule(context.Background(), r, file)

	require.NoError(t, err, "applyCallRule must no-op when no calls match")
	require.False(t, modified, "a rule that matched nothing or failed must not report a change")
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
	importAliases := ast.ImportAliasMap(file, nil)
	result, err := ip.applyCallAppendArgs(r, file, importAliases, nil)

	require.NoError(t, err)
	assert.False(t, result, "applyCallAppendArgs must return false when no calls match")
}

func TestApplyCallRule_WrapFailureReturnsError(t *testing.T) {
	// Template parses but generates invalid Go when applied to the matched call.
	file := makeCallFile(httpGetCall())
	r := httpGetRule("not a valid expression {{ . }}")

	modified, err := newTestPhase().applyCallRule(context.Background(), r, file)

	require.Error(t, err)
	require.False(t, modified, "a rule that matched nothing or failed must not report a change")
	assert.Contains(t, err.Error(), "failed to parse generated code")
}

func TestApplyIgnoredCallSites_BracketsAnnotatedStatement(t *testing.T) {
	root := parseFile(t, `package main

import "net/http"

func Run() {
	//otelc:ignore
	http.Get("ignored")
	http.Get("kept")
}
`)

	_, err := newTestPhase().applyIgnoredCallSites(context.Background(), root)
	require.NoError(t, err)

	src := renderFile(t, root)
	assert.Contains(
		t,
		src,
		"runtime.SuppressHooks()\n"+
			"\totelcIgnoreDone0 := false\n"+
			"\tdefer func() {\n"+
			"\t\tif !otelcIgnoreDone0 {\n"+
			"\t\t\truntime.UnsuppressHooks()\n"+
			"\t\t}\n"+
			"\t}()\n"+
			"\t//otelc:ignore\n"+
			"\thttp.Get(\"ignored\")\n"+
			"\totelcIgnoreDone0 = true\n"+
			"\truntime.UnsuppressHooks()",
	)
	assert.NotContains(t, src, "runtime.SuppressHooks()\n\thttp.Get(\"kept\")")
}

func TestApplyIgnoredCallSites_NoDirectiveNoChange(t *testing.T) {
	root := parseFile(t, `package main

import "net/http"

func Run() {
	http.Get("kept")
}
`)

	_, err := newTestPhase().applyIgnoredCallSites(context.Background(), root)
	require.NoError(t, err)

	src := renderFile(t, root)
	assert.NotContains(t, src, "SuppressHooks")
	assert.NotContains(t, src, `"runtime"`)
}

func TestApplyIgnoredCallSites_BracketsInsideNestedBlock(t *testing.T) {
	root := parseFile(t, `package main

func Run() {
	if true {
		//otelc:ignore
		hooked()
	}
}

func hooked() {}
`)

	_, err := newTestPhase().applyIgnoredCallSites(context.Background(), root)
	require.NoError(t, err)

	src := renderFile(t, root)
	assert.Contains(
		t,
		src,
		"runtime.SuppressHooks()\n"+
			"\t\totelcIgnoreDone0 := false\n"+
			"\t\tdefer func() {\n"+
			"\t\t\tif !otelcIgnoreDone0 {\n"+
			"\t\t\t\truntime.UnsuppressHooks()\n"+
			"\t\t\t}\n"+
			"\t\t}()\n"+
			"\t\t//otelc:ignore\n"+
			"\t\thooked()\n"+
			"\t\totelcIgnoreDone0 = true\n"+
			"\t\truntime.UnsuppressHooks()",
	)
}

func TestApplyIgnoredCallSites_RejectsBareReturn(t *testing.T) {
	root := parseFile(t, `package main

func Run() error {
	//otelc:ignore
	return hooked()
}

func hooked() error { return nil }
`)

	_, err := newTestPhase().applyIgnoredCallSites(context.Background(), root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "returns, breaks, continues, or jumps out of")
}

func TestApplyIgnoredCallSites_RejectsBreakInside(t *testing.T) {
	root := parseFile(t, `package main

func Run() {
	for {
		//otelc:ignore
		if hooked() {
			break
		}
	}
}

func hooked() bool { return false }
`)

	_, err := newTestPhase().applyIgnoredCallSites(context.Background(), root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "returns, breaks, continues, or jumps out of")
}

func TestApplyIgnoredCallSites_RejectsMultipleCallsInOneStatement(t *testing.T) {
	root := parseFile(t, `package main

func Run() {
	//otelc:ignore
	combine(a(), b())
}

func combine(x, y int) int { return x + y }
func a() int                { return 1 }
func b() int                { return 2 }
`)

	_, err := newTestPhase().applyIgnoredCallSites(context.Background(), root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "holds more than one call")
}

func TestApplyIgnoredCallSites_AllowsReturnInsideNestedClosure(t *testing.T) {
	root := parseFile(t, `package main

func Run() {
	//otelc:ignore
	wrap(func() bool {
		return hooked()
	})
}

func wrap(f func() bool) {}
func hooked() bool       { return false }
`)

	_, err := newTestPhase().applyIgnoredCallSites(context.Background(), root)
	require.NoError(t, err)

	src := renderFile(t, root)
	assert.Contains(t, src, "runtime.SuppressHooks()")
}

func TestApplyIgnoredCallSites_PreservesAssignmentScope(t *testing.T) {
	root := parseFile(t, `package main

func Run() {
	//otelc:ignore
	v, err := hooked()
	_ = v
	_ = err
}

func hooked() (int, error) { return 0, nil }
`)

	_, err := newTestPhase().applyIgnoredCallSites(context.Background(), root)
	require.NoError(t, err)

	src := renderFile(t, root)
	assert.Contains(t, src, "v, err := hooked()")
	assert.Contains(t, src, "_ = v")
	assert.Contains(t, src, "_ = err")
}

func TestApplyIgnoredCallSites_DistinctGuardNamesAcrossStatements(t *testing.T) {
	root := parseFile(t, `package main

func Run() {
	//otelc:ignore
	hooked()
	//otelc:ignore
	hooked()
}

func hooked() {}
`)

	_, err := newTestPhase().applyIgnoredCallSites(context.Background(), root)
	require.NoError(t, err)

	src := renderFile(t, root)
	assert.Contains(t, src, "otelcIgnoreDone0")
	assert.Contains(t, src, "otelcIgnoreDone1")
}

func TestApplyIgnoredCallSites_BracketsEvenWhenAWrapCallRuleAlsoSkippedTheCall(t *testing.T) {
	root := parseFile(t, `package main

import "unsafe"

func Run() {
	x := 42
	//otelc:ignore
	_ = unsafe.Sizeof(x)
}
`)

	ip := newTestPhase()
	r := &rule.InstCallRule{
		InstBaseRule: rule.InstBaseRule{Name: "wrap_sizeof"},
		FunctionCall: "unsafe.Sizeof",
		ImportPath:   "unsafe",
		FuncName:     "Sizeof",
		Replace:      "Wrapper({{ . }})",
	}
	_, err := ip.applyCallRule(context.Background(), r, root)
	require.NoError(t, err)

	_, err = ip.applyIgnoredCallSites(context.Background(), root)
	require.NoError(t, err)

	src := renderFile(t, root)
	assert.Contains(t, src, "runtime.SuppressHooks()")
	assert.NotContains(t, src, "Wrapper(")
}

func TestApplyIgnoredCallSites_BracketsSwitchCaseBody(t *testing.T) {
	root := parseFile(t, `package main

func Run() {
	switch true {
	case true:
		//otelc:ignore
		hooked()
	}
}

func hooked() {}
`)

	_, err := newTestPhase().applyIgnoredCallSites(context.Background(), root)
	require.NoError(t, err)

	src := renderFile(t, root)
	assert.Contains(t, src, "runtime.SuppressHooks()")
	assert.Contains(t, src, `"runtime"`)
}

func TestApplyIgnoredCallSites_BracketsSelectCommBody(t *testing.T) {
	root := parseFile(t, `package main

func Run(ch chan int) {
	select {
	case <-ch:
		//otelc:ignore
		hooked()
	}
}

func hooked() {}
`)

	_, err := newTestPhase().applyIgnoredCallSites(context.Background(), root)
	require.NoError(t, err)

	src := renderFile(t, root)
	assert.Contains(t, src, "runtime.SuppressHooks()")
	assert.Contains(t, src, `"runtime"`)
}

func TestApplyIgnoredCallSites_SelfImportUsesUnqualifiedCalls(t *testing.T) {
	root := parseFile(t, `package runtime

func Run() {
	//otelc:ignore
	hooked()
}

func hooked() {}
`)

	ip := newTestPhase()
	ip.compileArgs = []string{"-p", "runtime"}
	_, err := ip.applyIgnoredCallSites(context.Background(), root)
	require.NoError(t, err)

	src := renderFile(t, root)
	assert.Contains(
		t,
		src,
		"SuppressHooks()\n"+
			"\totelcIgnoreDone0 := false\n"+
			"\tdefer func() {\n"+
			"\t\tif !otelcIgnoreDone0 {\n"+
			"\t\t\tUnsuppressHooks()\n"+
			"\t\t}\n"+
			"\t}()\n"+
			"\t//otelc:ignore\n"+
			"\thooked()\n"+
			"\totelcIgnoreDone0 = true\n"+
			"\tUnsuppressHooks()",
	)
	assert.NotContains(t, src, `"runtime"`)
}

func TestApplyIgnoredCallSites_PackageClauseNamedRuntimeAtOtherImportPathIsQualified(t *testing.T) {
	root := parseFile(t, `package runtime

func Run() {
	//otelc:ignore
	hooked()
}

func hooked() {}
`)

	ip := newTestPhase()
	ip.compileArgs = []string{"-p", "example.com/vendored/runtime"}
	_, err := ip.applyIgnoredCallSites(context.Background(), root)
	require.NoError(t, err)

	src := renderFile(t, root)
	assert.Contains(
		t,
		src,
		"runtime.SuppressHooks()\n"+
			"\totelcIgnoreDone0 := false\n"+
			"\tdefer func() {\n"+
			"\t\tif !otelcIgnoreDone0 {\n"+
			"\t\t\truntime.UnsuppressHooks()\n"+
			"\t\t}\n"+
			"\t}()\n"+
			"\t//otelc:ignore\n"+
			"\thooked()\n"+
			"\totelcIgnoreDone0 = true\n"+
			"\truntime.UnsuppressHooks()",
	)
	assert.Contains(t, src, `"runtime"`)
}

func TestHasEscapingControlFlow(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		expected bool
	}{
		{
			name:     "plain call",
			src:      `hooked()`,
			expected: false,
		},
		{
			name:     "bare return",
			src:      `return hooked()`,
			expected: true,
		},
		{
			name:     "break",
			src:      `break`,
			expected: true,
		},
		{
			name:     "continue",
			src:      `continue`,
			expected: true,
		},
		{
			name:     "return inside func literal is scoped to it",
			src:      `wrap(func() bool { return hooked() })`,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := parseFile(t, "package main\nfunc Run() {\n"+tt.src+"\n}\n")
			fn := findFuncDeclInFile(t, root, "Run")
			stmt := fn.Body.List[0]
			assert.Equal(t, tt.expected, hasEscapingControlFlow(stmt))
		})
	}
}

func writeTempGoFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestPackageSourceFiles(t *testing.T) {
	dir := t.TempDir()
	goFile := writeTempGoFile(t, dir, "main.go", "package main\n")

	compileArgs := []string{
		"compile",
		"-p", "main",
		"-o", filepath.Join(dir, "main.a"),
		goFile,
		"not_a_go_file.txt",
	}

	files := packageSourceFiles(compileArgs)

	wantAbs, err := filepath.Abs(goFile)
	require.NoError(t, err)
	assert.Equal(t, []string{wantAbs}, files,
		"flags, their values, and non-Go-file args must all be skipped")
}

func TestPackageSourceFiles_NoGoFiles(t *testing.T) {
	compileArgs := []string{"compile", "-p", "main", "-complete"}

	files := packageSourceFiles(compileArgs)

	assert.Empty(t, files)
}

func TestPackageSourceFiles_RelativePathIsResolvedToAbsolute(t *testing.T) {
	dir := t.TempDir()
	writeTempGoFile(t, dir, "rel.go", "package main\n")

	t.Chdir(dir)

	files := packageSourceFiles([]string{"compile", "rel.go"})

	require.Len(t, files, 1)
	assert.True(t, filepath.IsAbs(files[0]), "relative source paths must be resolved to absolute")
	assert.Equal(t, "rel.go", filepath.Base(files[0]))
}

// selectorPosition returns the line and column, in that order, of the
// occurrence-th (1-indexed) "x.name(...)" selector it finds.
//
//nolint:revive // confusing-results conflicts with nonamedreturns
func selectorPosition(t *testing.T, path, name string, occurrence int) (int, int) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	require.NoError(t, err)

	var line, col int
	seen := 0
	goast.Inspect(f, func(n goast.Node) bool {
		sel, ok := n.(*goast.SelectorExpr)
		if !ok || sel.Sel.Name != name {
			return true
		}
		seen++
		if seen == occurrence {
			pos := fset.Position(sel.Sel.Pos())
			line, col = pos.Line, pos.Column
		}
		return true
	})
	require.GreaterOrEqual(t, seen, occurrence, "fewer than %d occurrences of selector %q", occurrence, name)
	return line, col
}

func TestCheckPackageForMethodCalls_ValueAndPointerReceiver(t *testing.T) {
	dir := t.TempDir()
	src := `package sample

type Logger struct{}

func (l Logger) Info(msg string)  {}
func (l *Logger) Warn(msg string) {}

type Embedder struct {
	Logger
}

func run() {
	var l Logger
	l.Info("hi")

	p := &Logger{}
	p.Warn("uh oh")

	var e Embedder
	e.Info("promoted")
}
`
	path := writeTempGoFile(t, dir, "sample.go", src)

	pi, err := checkPackageForMethodCalls("example.com/sample", []string{path}, imports.ImportConfig{})
	require.NoError(t, err)

	line, col := selectorPosition(t, path, "Info", 1) // l.Info("hi")
	importPath, recvType, ok := pi.methodReceiver(filepath.Base(path), line, col)
	require.True(t, ok)
	assert.Equal(t, "example.com/sample", importPath)
	assert.Equal(t, "Logger", recvType)

	line, col = selectorPosition(t, path, "Warn", 1)
	importPath, recvType, ok = pi.methodReceiver(filepath.Base(path), line, col)
	require.True(t, ok)
	assert.Equal(t, "example.com/sample", importPath)
	assert.Equal(t, "*Logger", recvType)

	// Promoted through embedding: the receiver is Logger, not Embedder.
	line, col = selectorPosition(t, path, "Info", 2)
	importPath, recvType, ok = pi.methodReceiver(filepath.Base(path), line, col)
	require.True(t, ok)
	assert.Equal(t, "example.com/sample", importPath)
	assert.Equal(t, "Logger", recvType)
}

func TestCheckPackageForMethodCalls_NonMethodSelectorDoesNotMatch(t *testing.T) {
	dir := t.TempDir()
	src := `package sample

type Point struct{ X int }

func run() {
	p := Point{X: 1}
	_ = p.X // plain field access, not a method call
}
`
	path := writeTempGoFile(t, dir, "sample.go", src)

	pi, err := checkPackageForMethodCalls("example.com/sample", []string{path}, imports.ImportConfig{})
	require.NoError(t, err)

	line, col := selectorPosition(t, path, "X", 1) // p.X
	_, _, ok := pi.methodReceiver(filepath.Base(path), line, col)
	assert.False(t, ok)
}

func TestCheckPackageForMethodCalls_MethodExpressionDoesNotMatch(t *testing.T) {
	// Method expressions (unbound) and bound method calls both share the
	// pkg.Method selector shape, but only the bound call match
	dir := t.TempDir()
	src := `package sample

type Logger struct{}

func (l Logger) Info(msg string) {}

func run() {
	var l Logger
	Logger.Info(l, "hi")
}
`
	path := writeTempGoFile(t, dir, "sample.go", src)

	pi, err := checkPackageForMethodCalls("example.com/sample", []string{path}, imports.ImportConfig{})
	require.NoError(t, err)

	line, col := selectorPosition(t, path, "Info", 1) // Logger.Info(l, "hi")
	_, _, ok := pi.methodReceiver(filepath.Base(path), line, col)
	assert.False(t, ok, "a method expression must not resolve as a bound method call")
}

func TestCheckPackageForMethodCalls_PositionMatchesAstParser(t *testing.T) {
	// Pins the position convention against tool/internal/ast.AstParser,
	// the parser the rewrite pass actually uses.
	dir := t.TempDir()
	src := `package sample

type Logger struct{}

func (l Logger) Info(msg string) {}

func run() {
	var l Logger
	l.Info("hi")
}
`
	path := writeTempGoFile(t, dir, "sample.go", src)

	p := ast.NewAstParser()
	root, err := p.Parse(path, 0)
	require.NoError(t, err)

	var line, col int
	dst.Inspect(root, func(n dst.Node) bool {
		sel, ok := n.(*dst.SelectorExpr)
		if !ok || sel.Sel.Name != "Info" {
			return true
		}
		pos := p.FindPosition(sel.Sel)
		line, col = pos.Line, pos.Column
		return false
	})
	require.NotZero(t, line, "did not find Info selector via dst")

	pi, err := checkPackageForMethodCalls("example.com/sample", []string{path}, imports.ImportConfig{})
	require.NoError(t, err)

	_, recvType, ok := pi.methodReceiver(filepath.Base(path), line, col)
	require.True(t, ok)
	assert.Equal(t, "Logger", recvType)
}

// testMethodCallPkgPath is the package path every method_call test in this
// file compiles its sample source as.
const testMethodCallPkgPath = "example.com/sample"

// methodCallRule builds an InstCallRule directly, bypassing YAML parsing.
func methodCallRule(recvType, funcName, replace string) *rule.InstCallRule {
	return &rule.InstCallRule{
		InstBaseRule: rule.InstBaseRule{Name: "wrap_method"},
		MethodCall:   testMethodCallPkgPath + "." + recvType + "." + funcName,
		ImportPath:   testMethodCallPkgPath,
		RecvType:     recvType,
		FuncName:     funcName,
		Replace:      replace,
	}
}

// setupMethodCallPhase parses source through parseFile so the dst and type-checking passes see the same file.
func setupMethodCallPhase(t *testing.T, source string) (*instrumentPhase, *dst.File) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.go")
	require.NoError(t, os.WriteFile(path, []byte(source), 0o600))

	ip := &instrumentPhase{
		logger:      slog.New(slog.DiscardHandler),
		compileArgs: []string{"compile", "-p", testMethodCallPkgPath, "-o", filepath.Join(dir, "sample.a"), path},
	}
	root, err := ip.parseFile(path)
	require.NoError(t, err)
	return ip, root
}

func TestApplyCallRule_MethodCall_ValueReceiver(t *testing.T) {
	ip, root := setupMethodCallPhase(t, `package sample

type Logger struct{}

func (l Logger) Info(msg string) {}

func run() {
	var l Logger
	l.Info("hi")
}
`)
	r := methodCallRule("Logger", "Info", "traced({{ . }})")

	_, err := ip.applyCallRule(context.Background(), r, root)
	require.NoError(t, err)

	out := renderFile(t, root)
	assert.Contains(t, out, `traced(l.Info("hi"))`)
}

func TestApplyCallRule_MethodCall_PointerReceiver(t *testing.T) {
	ip, root := setupMethodCallPhase(t, `package sample

type DB struct{}

func (db *DB) QueryContext(q string) {}

func run() {
	db := &DB{}
	db.QueryContext("select 1")
}
`)
	r := methodCallRule("*DB", "QueryContext", "traced({{ . }})")

	_, err := ip.applyCallRule(context.Background(), r, root)
	require.NoError(t, err)

	out := renderFile(t, root)
	assert.Contains(t, out, `traced(db.QueryContext("select 1"))`)
}

func TestApplyCallRule_MethodCall_PointerRuleDoesNotMatchValueReceiver(t *testing.T) {
	// "*DB" must not match a value receiver, mirroring InstFuncRule's Recv.
	ip, root := setupMethodCallPhase(t, `package sample

type DB struct{}

func (db DB) QueryContext(q string) {}

func run() {
	var db DB
	db.QueryContext("select 1")
}
`)
	r := methodCallRule("*DB", "QueryContext", "traced({{ . }})")

	_, err := ip.applyCallRule(context.Background(), r, root)
	require.NoError(t, err)

	out := renderFile(t, root)
	assert.NotContains(t, out, "traced(")
}

func TestApplyCallRule_MethodCall_EmbeddedPromotedMethod(t *testing.T) {
	// Info is promoted from Logger to Embedder. The rule targets Logger.
	ip, root := setupMethodCallPhase(t, `package sample

type Logger struct{}

func (l Logger) Info(msg string) {}

type Embedder struct {
	Logger
}

func run() {
	var e Embedder
	e.Info("promoted")
}
`)
	r := methodCallRule("Logger", "Info", "traced({{ . }})")

	_, err := ip.applyCallRule(context.Background(), r, root)
	require.NoError(t, err)

	out := renderFile(t, root)
	assert.Contains(t, out, `traced(e.Info("promoted"))`)
}

func TestApplyCallRule_MethodCall_ChainedSelector(t *testing.T) {
	ip, root := setupMethodCallPhase(t, `package sample

type Client struct{}

func (c *Client) Do(req string) {}

type Wrapper struct {
	Client *Client
}

func run() {
	w := &Wrapper{Client: &Client{}}
	w.Client.Do("req")
}
`)
	r := methodCallRule("*Client", "Do", "traced({{ . }})")

	_, err := ip.applyCallRule(context.Background(), r, root)
	require.NoError(t, err)

	out := renderFile(t, root)
	assert.Contains(t, out, `traced(w.Client.Do("req"))`)
}

func TestApplyCallRule_MethodCall_WrongReceiverTypeDoesNotMatch(t *testing.T) {
	ip, root := setupMethodCallPhase(t, `package sample

type Logger struct{}
type OtherLogger struct{}

func (l Logger) Info(msg string)      {}
func (l OtherLogger) Info(msg string) {}

func run() {
	var o OtherLogger
	o.Info("hi")
}
`)
	var logs bytes.Buffer
	ip.logger = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r := methodCallRule("Logger", "Info", "traced({{ . }})")

	_, err := ip.applyCallRule(context.Background(), r, root)
	require.NoError(t, err)

	out := renderFile(t, root)
	assert.NotContains(t, out, "traced(")
	assert.Empty(t, logs.String(),
		"a call that resolves cleanly to a different type is a correct non-match, not a miss to log")
}

func TestApplyCallRule_MethodCall_NameMismatchSkipsTypeChecking(t *testing.T) {
	// A method-name mismatch must reject before ever building package type
	// info.
	ip, root := setupMethodCallPhase(t, `package sample

type Logger struct{}

func (l Logger) Warn(msg string) {}

func run() {
	var l Logger
	l.Warn("hi")
}
`)
	r := methodCallRule("Logger", "Info", "traced({{ . }})")

	_, err := ip.applyCallRule(context.Background(), r, root)
	require.NoError(t, err)

	assert.False(t, ip.methodCallInfoLoaded,
		"a call whose method name doesn't match the rule must never trigger type-checking")
}

func TestApplyCallRule_MethodCall_CachesPackageInfoAcrossRules(t *testing.T) {
	ip, root := setupMethodCallPhase(t, `package sample

type Logger struct{}

func (l Logger) Info(msg string) {}
func (l Logger) Warn(msg string) {}

func run() {
	var l Logger
	l.Info("hi")
	l.Warn("uh oh")
}
`)
	infoRule := methodCallRule("Logger", "Info", "tracedInfo({{ . }})")
	warnRule := methodCallRule("Logger", "Warn", "tracedWarn({{ . }})")

	_, err := ip.applyCallRule(context.Background(), infoRule, root)
	require.NoError(t, err)
	info := ip.methodCallInfo
	require.NotNil(t, info, "first method_call rule must have built package info")

	_, err = ip.applyCallRule(context.Background(), warnRule, root)
	require.NoError(t, err)
	assert.Same(t, info, ip.methodCallInfo,
		"a second method_call rule on the same package must reuse, not rebuild, the cached package info")

	out := renderFile(t, root)
	assert.Contains(t, out, `tracedInfo(l.Info("hi"))`)
	assert.Contains(t, out, `tracedWarn(l.Warn("uh oh"))`)
}

func TestApplyCallRule_MethodCall_TwoRulesWrapSameCall(t *testing.T) {
	ip, root := setupMethodCallPhase(t, `package sample

type Logger struct{}

func (l Logger) Info(msg string) {}

func run() {
	var l Logger
	l.Info("hi")
}
`)
	firstRule := methodCallRule("Logger", "Info", "first({{ . }})")
	secondRule := methodCallRule("Logger", "Info", "second({{ . }})")

	_, err := ip.applyCallRule(context.Background(), firstRule, root)
	require.NoError(t, err)
	_, err = ip.applyCallRule(context.Background(), secondRule, root)
	require.NoError(t, err)

	out := renderFile(t, root)
	assert.Contains(t, out, `first(second(l.Info("hi")))`)
}

func TestApplyCallRule_MethodCall_MethodExpressionDoesNotMatch(t *testing.T) {
	ip, root := setupMethodCallPhase(t, `package sample

type Logger struct{}

func (l Logger) Info(msg string) {}

func run() {
	var l Logger
	Logger.Info(l, "hi")
}
`)
	r := methodCallRule("Logger", "Info", "traced({{ . }})")

	_, err := ip.applyCallRule(context.Background(), r, root)
	require.NoError(t, err)

	out := renderFile(t, root)
	assert.NotContains(t, out, "traced(")
}

func TestMatchesMethodCallRule_TypeCheckFailureSkipsMatch(t *testing.T) {
	ip := &instrumentPhase{
		logger:      slog.New(slog.DiscardHandler),
		compileArgs: []string{"compile", "-p", testMethodCallPkgPath, filepath.Join(t.TempDir(), "missing.go")},
	}
	r := methodCallRule("Logger", "Info", "traced({{ . }})")

	call := &dst.CallExpr{
		Fun: &dst.SelectorExpr{
			X:   &dst.Ident{Name: "l"},
			Sel: &dst.Ident{Name: "Info"},
		},
	}

	assert.False(t, ip.matchesMethodCallRule(call, r))
	assert.True(t, ip.methodCallInfoLoaded)
	assert.Nil(t, ip.methodCallInfo)
}

func TestMatchesMethodCallRule_UnmappedSelectorSkipsMatch(t *testing.T) {
	ip, _ := setupMethodCallPhase(t, `package sample

type Logger struct{}

func (l Logger) Info(msg string) {}
`)
	r := methodCallRule("Logger", "Info", "traced({{ . }})")

	call := &dst.CallExpr{
		Fun: &dst.SelectorExpr{
			X:   &dst.Ident{Name: "l"},
			Sel: &dst.Ident{Name: "Info"},
		},
	}

	assert.False(t, ip.matchesMethodCallRule(call, r))
}

func TestMatchesMethodCallRule_NoSourcePositionLogsMiss(t *testing.T) {
	ip, _ := setupMethodCallPhase(t, `package sample

type Logger struct{}

func (l Logger) Info(msg string) {}
`)
	var logs bytes.Buffer
	ip.logger = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r := methodCallRule("Logger", "Info", "traced({{ . }})")

	call := &dst.CallExpr{
		Fun: &dst.SelectorExpr{
			X:   &dst.Ident{Name: "l"},
			Sel: &dst.Ident{Name: "Info"},
		},
	}

	assert.False(t, ip.matchesMethodCallRule(call, r))
	assert.Contains(t, logs.String(), "no source position")
}

// findCallByMethodName returns the first *dst.CallExpr in root whose Fun is a
// selector naming methodName, e.g. recv.methodName(...).
func findCallByMethodName(root *dst.File, methodName string) *dst.CallExpr {
	var found *dst.CallExpr
	dst.Inspect(root, func(n dst.Node) bool {
		if found != nil {
			return false
		}
		call, ok := n.(*dst.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*dst.SelectorExpr)
		if !ok || sel.Sel.Name != methodName {
			return true
		}
		found = call
		return false
	})
	return found
}

func TestMatchesMethodCallRule_UnresolvedReceiverLogsMiss(t *testing.T) {
	ip, root := setupMethodCallPhase(t, `package sample

type Writer interface {
	Write(p []byte) (int, error)
}

type Buffer struct{}

func (b *Buffer) Write(p []byte) (int, error) { return len(p), nil }

func run() {
	var w Writer = &Buffer{}
	w.Write(nil)
}
`)
	var logs bytes.Buffer
	ip.logger = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r := methodCallRule("*Buffer", "Write", "traced({{ . }})")

	call := findCallByMethodName(root, "Write")
	require.NotNil(t, call, "did not find Write call via dst")

	assert.False(t, ip.matchesMethodCallRule(call, r),
		"a call through an interface-typed receiver must not resolve, so it must not match")
	assert.Contains(t, logs.String(), "could not be resolved to a receiver type")
}

func TestMatchesMethodCallRule_MissLogsFirstTypeError(t *testing.T) {
	// undefinedFn makes the package check record a firstTypeError while
	// still resolving the rest of the package. The call l.Info cannot
	// resolve because l is undeclared, so the miss log must carry the
	// recorded type error.
	ip, root := setupMethodCallPhase(t, `package sample

type Logger struct{}

func (l Logger) Info(msg string) {}

func run() {
	undefinedFn()
	l.Info("hi")
}
`)
	var logs bytes.Buffer
	ip.logger = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r := methodCallRule("Logger", "Info", "traced({{ . }})")

	call := findCallByMethodName(root, "Info")
	require.NotNil(t, call, "did not find Info call via dst")

	assert.False(t, ip.matchesMethodCallRule(call, r))
	assert.Contains(t, logs.String(), "could not be resolved to a receiver type")
	assert.Contains(t, logs.String(), "first_type_error")
	assert.Contains(t, logs.String(), "undefined")
}

func TestCheckPackageForMethodCalls_NonexistentFileFails(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.go")

	_, err := checkPackageForMethodCalls("example.com/sample", []string{missing}, imports.ImportConfig{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "opening")
}

func TestCheckPackageForMethodCalls_SyntaxErrorFails(t *testing.T) {
	dir := t.TempDir()
	path := writeTempGoFile(t, dir, "sample.go", `package sample

func run( {
`)

	_, err := checkPackageForMethodCalls("example.com/sample", []string{path}, imports.ImportConfig{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing")
}

func TestCheckPackageForMethodCalls_TypeErrorIsIgnored(t *testing.T) {
	dir := t.TempDir()
	path := writeTempGoFile(t, dir, "sample.go", `package sample

type Logger struct{}

func (l Logger) Info(msg string) {}

func run() {
	var l Logger
	l.Info("hi")
	var s string = 1
	_ = s
}
`)

	pi, err := checkPackageForMethodCalls("example.com/sample", []string{path}, imports.ImportConfig{})
	require.NoError(t, err)

	line, col := selectorPosition(t, path, "Info", 1)
	_, recvType, ok := pi.methodReceiver(filepath.Base(path), line, col)
	require.True(t, ok, "a type error elsewhere must not prevent resolving unrelated selections")
	assert.Equal(t, "Logger", recvType)
	require.Error(t, pi.firstTypeError, "the type error must still be recorded for later diagnostics")
}

func TestMethodReceiver_PositionNotFound(t *testing.T) {
	pi := &methodCallPackageInfo{selByPos: map[methodCallPosition]*types.Selection{}}

	_, _, ok := pi.methodReceiver("sample.go", 1, 1)

	assert.False(t, ok)
}

func TestMethodReceiver_AnonymousInterfaceReceiverDoesNotMatch(t *testing.T) {
	dir := t.TempDir()
	path := writeTempGoFile(t, dir, "sample.go", `package sample

func run() {
	var a interface{ M() }
	a.M()
}
`)

	pi, err := checkPackageForMethodCalls("example.com/sample", []string{path}, imports.ImportConfig{})
	require.NoError(t, err)

	line, col := selectorPosition(t, path, "M", 1)
	_, _, ok := pi.methodReceiver(filepath.Base(path), line, col)
	assert.False(t, ok, "an anonymous interface receiver has no named type to report")
}

func TestMethodReceiver_NamedInterfaceReceiverDoesNotMatch(t *testing.T) {
	dir := t.TempDir()
	path := writeTempGoFile(t, dir, "sample.go", `package sample

type Writer interface {
	Write(p []byte) (int, error)
}

type Buffer struct{}

func (b *Buffer) Write(p []byte) (int, error) { return len(p), nil }

func run() {
	var w Writer = &Buffer{}
	w.Write(nil)
}
`)

	pi, err := checkPackageForMethodCalls("example.com/sample", []string{path}, imports.ImportConfig{})
	require.NoError(t, err)

	line, col := selectorPosition(t, path, "Write", 1)
	_, _, ok := pi.methodReceiver(filepath.Base(path), line, col)
	assert.False(t, ok,
		"a call through a named interface-typed receiver must not resolve, since go/types reports "+
			"the interface as the receiver, not the concrete *Buffer behind it")
}

// buildFakeArchive writes a minimal cmd/compile-style archive file
func buildFakeArchive(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	const objapi = "go object go1.99\n"
	const marker = "$$B\n"
	const trailer = "\n$$\n"
	payload := objapi + marker + string(data) + trailer

	header := make([]byte, 60)
	copy(header, []byte("__.PKGDEF"))
	sizeStr := strconv.Itoa(len(payload))
	copy(header[48:58], []byte(sizeStr))
	for i := 48 + len(sizeStr); i < 58; i++ {
		header[i] = ' '
	}
	header[58] = '`'
	header[59] = '\n'

	var buf bytes.Buffer
	buf.WriteString("!<arch>\n")
	buf.Write(header)
	buf.WriteString(payload)

	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o600))
	return path
}

func TestExportImporter_Import_Unsafe(t *testing.T) {
	imp := newExportImporter(token.NewFileSet(), nil, nil)

	pkg, err := imp.Import("unsafe")

	require.NoError(t, err)
	assert.Same(t, types.Unsafe, pkg)
}

func TestExportImporter_ImportFrom_MissingArchive(t *testing.T) {
	imp := newExportImporter(token.NewFileSet(), nil, nil)

	_, err := imp.ImportFrom("example.com/missing", "", 0)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no archive for import path")
}

func TestExportImporter_ImportFrom_OpenArchiveError(t *testing.T) {
	dir := t.TempDir()
	imp := newExportImporter(token.NewFileSet(), map[string]string{
		"example.com/gone": filepath.Join(dir, "does-not-exist.a"),
	}, nil)

	_, err := imp.ImportFrom("example.com/gone", "", 0)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "opening archive")
}

func TestExportImporter_ImportFrom_NewReaderError(t *testing.T) {
	dir := t.TempDir()
	path := writeTempGoFile(t, dir, "notanarchive.a", "not an archive at all")
	imp := newExportImporter(token.NewFileSet(), map[string]string{"example.com/bad": path}, nil)

	_, err := imp.ImportFrom("example.com/bad", "", 0)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading export data section")
}

func TestExportImporter_ImportFrom_DecodeError(t *testing.T) {
	dir := t.TempDir()
	path := buildFakeArchive(t, dir, "bad.a", []byte("Z"))
	imp := newExportImporter(token.NewFileSet(), map[string]string{"example.com/bad": path}, nil)

	_, err := imp.ImportFrom("example.com/bad", "", 0)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "decoding export data")
}

func TestExportImporter_ImportFrom_SuccessAndCache(t *testing.T) {
	dir := t.TempDir()

	fset := token.NewFileSet()
	pkg := types.NewPackage("example.com/fake", "fake")
	pkg.MarkComplete()
	var data bytes.Buffer
	require.NoError(t, gcexportdata.Write(&data, fset, pkg))

	path := buildFakeArchive(t, dir, "fake.a", data.Bytes())
	imp := newExportImporter(token.NewFileSet(), map[string]string{"example.com/fake": path}, nil)

	got, err := imp.ImportFrom("example.com/fake", "", 0)
	require.NoError(t, err)
	assert.Equal(t, "example.com/fake", got.Path())
	assert.True(t, got.Complete())

	require.NoError(t, os.Remove(path))

	got2, err := imp.ImportFrom("example.com/fake", "", 0)
	require.NoError(t, err, "a cached complete package must not be re-read from disk")
	assert.Same(t, got, got2)
}

func TestExportImporter_ImportFrom_ImportMapFallback(t *testing.T) {
	dir := t.TempDir()

	fset := token.NewFileSet()
	pkg := types.NewPackage("vendor/golang.org/x/net/http2/hpack", "hpack")
	pkg.MarkComplete()
	var data bytes.Buffer
	require.NoError(t, gcexportdata.Write(&data, fset, pkg))

	path := buildFakeArchive(t, dir, "hpack.a", data.Bytes())
	imp := newExportImporter(
		token.NewFileSet(),
		map[string]string{"vendor/golang.org/x/net/http2/hpack": path},
		map[string]string{"golang.org/x/net/http2/hpack": "vendor/golang.org/x/net/http2/hpack"},
	)

	got, err := imp.ImportFrom("golang.org/x/net/http2/hpack", "", 0)
	require.NoError(t, err)
	assert.Equal(t, "vendor/golang.org/x/net/http2/hpack", got.Path())
	assert.True(t, got.Complete())
}

// TestApplyCallRule_UnaliasedImportWithDivergentNameMatches covers a
// package whose declared name differs from the name guessed from its
// import path, for example "redis" for "github.com/redis/go-redis/v9".
// With ip.importNames holding the real name, matching must use that
// name instead of the guess.
func TestApplyCallRule_UnaliasedImportWithDivergentNameMatches(t *testing.T) {
	const importPath = "github.com/redis/go-redis/v9"
	root := parseFile(t, `package main

import "`+importPath+`"

func Run() {
	redis.NewClient()
}
`)
	r := &rule.InstCallRule{
		InstBaseRule: rule.InstBaseRule{Name: "wrap_new_client"},
		ImportPath:   importPath,
		FuncName:     "NewClient",
		Replace:      "traced({{ . }})",
	}

	ip := newTestPhase()
	ip.importNames = map[string]string{importPath: "redis"}

	_, err := ip.applyCallRule(context.Background(), r, root)

	require.NoError(t, err)
	run := findFuncDeclInFile(t, root, "Run")
	stmt := run.Body.List[0].(*dst.ExprStmt)
	call, ok := stmt.X.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr after wrap, got %T", stmt.X)
	fn, ok := call.Fun.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "traced", fn.Name, "rule must have matched and wrapped the call")
}

func TestApplyCallRule_UnaliasedImportWithDivergentNameMissesWithoutTable(t *testing.T) {
	// This test uses the same fixture, without ip.importNames. Matching
	// reverts to the guess "go-redis" and must silently miss the call.
	const importPath = "github.com/redis/go-redis/v9"
	root := parseFile(t, `package main

import "`+importPath+`"

func Run() {
	redis.NewClient()
}
`)
	r := &rule.InstCallRule{
		InstBaseRule: rule.InstBaseRule{Name: "wrap_new_client"},
		ImportPath:   importPath,
		FuncName:     "NewClient",
		Replace:      "traced({{ . }})",
	}

	_, err := newTestPhase().applyCallRule(context.Background(), r, root)

	require.NoError(t, err)
	run := findFuncDeclInFile(t, root, "Run")
	stmt := run.Body.List[0].(*dst.ExprStmt)
	_, ok := stmt.X.(*dst.CallExpr)
	require.True(t, ok)
	sel, ok := stmt.X.(*dst.CallExpr).Fun.(*dst.SelectorExpr)
	require.True(t, ok, "call must be left unwrapped (still redis.NewClient()), got %T", stmt.X.(*dst.CallExpr).Fun)
	assert.Equal(t, "NewClient", sel.Sel.Name)
}

func TestApplyCallRule_AliasOverrideDoesNotResolvePackages(t *testing.T) {
	root := parseFile(t, `package main

import (
	f "example.com/does/not/exist"
	"net/http"
)

func Run() {
	http.Get("url")
}
`)
	r := httpGetRule("traced.Call({{ . }})")
	r.Imports = map[string]string{"traced": "example.com/does/not/exist"}

	_, err := newTestPhase().applyCallRule(context.Background(), r, root)

	require.NoError(t, err)
	run := findFuncDeclInFile(t, root, "Run")
	stmt := run.Body.List[0].(*dst.ExprStmt)
	call, ok := stmt.X.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr after wrap, got %T", stmt.X)
	sel, ok := call.Fun.(*dst.SelectorExpr)
	require.True(t, ok, "expected *dst.SelectorExpr, got %T", call.Fun)
	ident, ok := sel.X.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "f", ident.Name, "override must use the file's existing alias without a live package lookup")
}

func TestApplyCallRule_AliasOverrideUsesResolvedName(t *testing.T) {
	const importPath = "github.com/redis/go-redis/v9"
	root := parseFile(t, `package main

import "`+importPath+`"

func Run() {
	redis.NewClient()
}
`)
	r := &rule.InstCallRule{
		InstBaseRule: rule.InstBaseRule{
			Name:    "wrap_new_client",
			Imports: map[string]string{"traced": importPath},
		},
		ImportPath: importPath,
		FuncName:   "NewClient",
		Replace:    "traced.Wrap({{ . }})",
	}

	ip := newTestPhase()
	ip.importNames = map[string]string{importPath: "redis"}

	_, err := ip.applyCallRule(context.Background(), r, root)

	require.NoError(t, err)
	run := findFuncDeclInFile(t, root, "Run")
	stmt := run.Body.List[0].(*dst.ExprStmt)
	call, ok := stmt.X.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr after wrap, got %T", stmt.X)
	sel, ok := call.Fun.(*dst.SelectorExpr)
	require.True(t, ok, "expected *dst.SelectorExpr, got %T", call.Fun)
	ident, ok := sel.X.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "redis", ident.Name, "override must use the resolved real name, not the path-derived guess")
	assert.Equal(t, "Wrap", sel.Sel.Name)
	assert.Equal(t, 1, countImportSpecs(root), "must not add a redundant import for an alias the rewrite eliminated")
}
