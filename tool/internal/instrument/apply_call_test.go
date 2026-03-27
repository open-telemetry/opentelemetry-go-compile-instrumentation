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

func TestApplyCallRule_NoMatch(t *testing.T) {
	// Rule targets net/http.Post; file has net/http.Get — no match.
	file := makeCallFile(httpGetCall())
	r := &rule.InstCallRule{
		InstBaseRule: rule.InstBaseRule{Name: "wrap_post"},
		FunctionCall: "net/http.Post",
		ImportPath:   "net/http",
		FuncName:     "Post",
		Template:     "traced({{ . }})",
	}

	err := newTestPhase().applyCallRule(context.Background(), r, file)

	require.NoError(t, err)
	// Expression must be unchanged.
	stmt := file.Decls[0].(*dst.FuncDecl).Body.List[0].(*dst.ExprStmt)
	call, ok := stmt.X.(*dst.CallExpr)
	require.True(t, ok)
	sel, ok := call.Fun.(*dst.SelectorExpr)
	require.True(t, ok)
	assert.Equal(t, "Get", sel.Sel.Name)
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
