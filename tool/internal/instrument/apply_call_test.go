// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"go/token"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/instrument/template"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
)

func TestWrapCall_Success(t *testing.T) {
	// Create a rule with a simple template
	r := &rule.InstCallRule{
		Template: "wrapper({{ . }})",
	}
	tmpl, err := template.NewTemplate(r.Template)
	require.NoError(t, err)
	r.CompiledTemplate = tmpl

	// Create a call expression
	call := &dst.CallExpr{
		Fun: &dst.Ident{Name: "original"},
	}

	// Wrap it
	err = wrapCall(call, r)

	// Verify - the call expression is modified in place
	require.NoError(t, err)
	// After wrapping, the outer call is now "wrapper"
	wrapperIdent, ok := call.Fun.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "wrapper", wrapperIdent.Name)
	// Should have exactly one argument (the original call)
	require.Len(t, call.Args, 1)
	// Verify the argument is a call expression (structure preserved)
	_, ok = call.Args[0].(*dst.CallExpr)
	require.True(t, ok, "expected inner argument to be a call expression")
}

func TestWrapCall_NilTemplate(t *testing.T) {
	r := &rule.InstCallRule{
		Template:         "wrapper({{ . }})",
		CompiledTemplate: nil, // No template
	}

	call := &dst.CallExpr{
		Fun: &dst.Ident{Name: "test"},
	}

	err := wrapCall(call, r)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no compiled template")
}

func TestWrapCall_TemplateCompilationError(t *testing.T) {
	// Create a rule with a template that produces invalid Go syntax
	r := &rule.InstCallRule{
		Template: "func {{ . }}", // "func" keyword without proper syntax
	}
	tmpl, err := template.NewTemplate(r.Template)
	require.NoError(t, err)
	r.CompiledTemplate = tmpl

	call := &dst.CallExpr{
		Fun: &dst.Ident{Name: "test"},
	}

	err = wrapCall(call, r)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to compile template")
}

func TestWrapCall_NonCallExpressionResult(t *testing.T) {
	// Create a template that produces a non-call expression
	r := &rule.InstCallRule{
		Template: "{{ . }}.Field",
	}
	tmpl, err := template.NewTemplate(r.Template)
	require.NoError(t, err)
	r.CompiledTemplate = tmpl

	call := &dst.CallExpr{
		Fun: &dst.Ident{Name: "test"},
	}

	err = wrapCall(call, r)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not produce a call expression")
}

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
