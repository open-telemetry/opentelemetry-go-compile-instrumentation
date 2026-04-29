// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"go/token"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
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

func httpGetRule(template string) *rule.InstCallRule {
	return &rule.InstCallRule{
		InstBaseRule: rule.InstBaseRule{Name: "wrap_get"},
		FunctionCall: "net/http.Get",
		ImportPath:   "net/http",
		FuncName:     "Get",
		Template:     template,
	}
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
	// Template produces a selector expression, not a call expression.
	file := makeCallFile(httpGetCall())
	r := httpGetRule("{{ . }}.Response")

	err := newTestPhase().applyCallRule(context.Background(), r, file)

	require.NoError(t, err)
	stmt := file.Decls[0].(*dst.FuncDecl).Body.List[0].(*dst.ExprStmt)
	_, ok := stmt.X.(*dst.SelectorExpr)
	require.True(t, ok, "expected *dst.SelectorExpr after wrap, got %T", stmt.X)
}

func TestApplyCallRule_InvalidTemplate(t *testing.T) {
	// An unclosed template tag fails fasttemplate parsing in newCallTemplate.
	file := makeCallFile(httpGetCall())
	r := httpGetRule("wrapper({{")

	err := newTestPhase().applyCallRule(context.Background(), r, file)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "rule has no compiled template")
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

	importAliases := collectImportAliases(file)
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

func TestAppendCallArgs_WithTemplate(t *testing.T) {
	// Both append_args and template: args appended first, then template wraps.
	call := httpGetCall()
	file := makeCallFile(call)
	r := &rule.InstCallRule{
		InstBaseRule: rule.InstBaseRule{Name: "wrap_get"},
		FunctionCall: "net/http.Get",
		ImportPath:   "net/http",
		FuncName:     "Get",
		AppendArgs:   []string{"42"},
		Template:     "wrapper({{ . }})",
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

	importAliases := collectImportAliases(file)
	matches := matchesCallRule(call, r, importAliases)

	assert.True(t, matches)
}
