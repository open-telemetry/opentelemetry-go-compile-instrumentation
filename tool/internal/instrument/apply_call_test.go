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

	file := &dst.File{}
	matches := matchesCallRule(call, r, file)

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

	file := &dst.File{}
	matches := matchesCallRule(call, r, file)

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

	file := &dst.File{}
	matches := matchesCallRule(call, r, file)

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

	file := &dst.File{}
	matches := matchesCallRule(call, r, file)

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

	file := &dst.File{}
	matches := matchesCallRule(call, r, file)

	assert.False(t, matches)
}

func TestAddImportsFromRule_AddNewImport(t *testing.T) {
	root := &dst.File{
		Name:  dst.NewIdent("main"),
		Decls: []dst.Decl{},
	}

	r := &rule.InstCallRule{
		Imports: map[string]string{
			"unsafe": "unsafe",
		},
	}

	addImportsFromRule(root, r)

	// Verify import was added
	require.Len(t, root.Decls, 1)
	genDecl, ok := root.Decls[0].(*dst.GenDecl)
	require.True(t, ok)
	assert.Equal(t, token.IMPORT, genDecl.Tok)

	require.Len(t, genDecl.Specs, 1)
	importSpec, ok := genDecl.Specs[0].(*dst.ImportSpec)
	require.True(t, ok)
	assert.Equal(t, `"unsafe"`, importSpec.Path.Value)
	require.NotNil(t, importSpec.Name)
	assert.Equal(t, "unsafe", importSpec.Name.Name)
}

func TestAddImportsFromRule_SkipExistingImport(t *testing.T) {
	// File already has the import
	root := &dst.File{
		Name: dst.NewIdent("main"),
		Decls: []dst.Decl{
			&dst.GenDecl{
				Tok: token.IMPORT,
				Specs: []dst.Spec{
					&dst.ImportSpec{
						Path: &dst.BasicLit{Value: `"unsafe"`},
						Name: &dst.Ident{Name: "unsafe"},
					},
				},
			},
		},
	}

	r := &rule.InstCallRule{
		Imports: map[string]string{
			"unsafe": "unsafe",
		},
	}

	addImportsFromRule(root, r)

	// Verify no duplicate import was added
	assert.Len(t, root.Decls, 1, "should not add duplicate import")
}

func TestAddImportsFromRule_HandleImportWithAlias(t *testing.T) {
	root := &dst.File{
		Name:  dst.NewIdent("main"),
		Decls: []dst.Decl{},
	}

	r := &rule.InstCallRule{
		Imports: map[string]string{
			"myalias": "some/package",
		},
	}

	addImportsFromRule(root, r)

	// Verify import with alias was added
	require.Len(t, root.Decls, 1)
	genDecl := root.Decls[0].(*dst.GenDecl)
	importSpec := genDecl.Specs[0].(*dst.ImportSpec)

	assert.Equal(t, `"some/package"`, importSpec.Path.Value)
	assert.Equal(t, "myalias", importSpec.Name.Name)
}

func TestAddImportsFromRule_NoImports(t *testing.T) {
	root := &dst.File{
		Name:  dst.NewIdent("main"),
		Decls: []dst.Decl{},
	}

	r := &rule.InstCallRule{
		Imports: nil, // No imports
	}

	addImportsFromRule(root, r)

	// Verify no imports were added
	assert.Empty(t, root.Decls)
}

func TestAddImportsFromRule_EmptyImports(t *testing.T) {
	root := &dst.File{
		Name:  dst.NewIdent("main"),
		Decls: []dst.Decl{},
	}

	r := &rule.InstCallRule{
		Imports: map[string]string{}, // Empty map
	}

	addImportsFromRule(root, r)

	// Verify no imports were added
	assert.Empty(t, root.Decls)
}

func TestAddImportsFromRule_ExistingImportDifferentAlias(t *testing.T) {
	// File has import with different alias
	root := &dst.File{
		Name: dst.NewIdent("main"),
		Decls: []dst.Decl{
			&dst.GenDecl{
				Tok: token.IMPORT,
				Specs: []dst.Spec{
					&dst.ImportSpec{
						Path: &dst.BasicLit{Value: `"some/package"`},
						Name: &dst.Ident{Name: "existingalias"},
					},
				},
			},
		},
	}

	r := &rule.InstCallRule{
		Imports: map[string]string{
			"newalias": "some/package", // Same package, different alias
		},
	}

	addImportsFromRule(root, r)

	// Should not add duplicate (continues on alias mismatch)
	assert.Len(t, root.Decls, 1, "should not add duplicate import even with different alias")
}

func TestAddImportsFromRule_MultipleImports(t *testing.T) {
	root := &dst.File{
		Name:  dst.NewIdent("main"),
		Decls: []dst.Decl{},
	}

	r := &rule.InstCallRule{
		Imports: map[string]string{
			"unsafe": "unsafe",
			"fmt":    "fmt",
			"http":   "net/http",
		},
	}

	addImportsFromRule(root, r)

	// Verify all imports were added (order may vary due to map iteration)
	assert.Len(t, root.Decls, 3)
	for _, decl := range root.Decls {
		genDecl, ok := decl.(*dst.GenDecl)
		require.True(t, ok)
		assert.Equal(t, token.IMPORT, genDecl.Tok)
	}
}

func TestAddImportsFromRule_WithExistingCode(t *testing.T) {
	// File has existing declarations
	root := &dst.File{
		Name: dst.NewIdent("main"),
		Decls: []dst.Decl{
			&dst.FuncDecl{
				Name: dst.NewIdent("main"),
			},
		},
	}

	r := &rule.InstCallRule{
		Imports: map[string]string{
			"unsafe": "unsafe",
		},
	}

	addImportsFromRule(root, r)

	// Verify import was added at the beginning
	require.Len(t, root.Decls, 2)
	genDecl, ok := root.Decls[0].(*dst.GenDecl)
	require.True(t, ok)
	assert.Equal(t, token.IMPORT, genDecl.Tok)

	// Original function should be second
	funcDecl, ok := root.Decls[1].(*dst.FuncDecl)
	require.True(t, ok)
	assert.Equal(t, "main", funcDecl.Name.Name)
}
