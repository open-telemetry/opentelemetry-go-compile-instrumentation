// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"go/token"
	"io"
	"log/slog"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
)

// newTestPhase returns a minimal instrumentPhase suitable for unit tests that
// do not exercise import injection or compilation (logger discards all output).
func newTestPhase() *instrumentPhase {
	return &instrumentPhase{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// countImportSpecs counts the import specs declared in the file. Import
// injection appends to root.Decls rather than root.Imports, so the decls are
// what a file written back out actually reflects.
func countImportSpecs(root *dst.File) int {
	count := 0
	for _, decl := range root.Decls {
		genDecl, ok := decl.(*dst.GenDecl)
		if !ok || genDecl.Tok != token.IMPORT {
			continue
		}
		count += len(genDecl.Specs)
	}
	return count
}

// --- wrapDeclValue helper tests ---

func TestWrapDeclValue_Success(t *testing.T) {
	// Simulate: var X = someCall()
	spec := &dst.ValueSpec{
		Names: []*dst.Ident{{Name: "X"}},
		Values: []dst.Expr{
			&dst.CallExpr{Fun: &dst.Ident{Name: "someCall"}},
		},
	}

	err := wrapDeclValue(spec, "wrapper({{ . }})", 0)

	require.NoError(t, err)
	require.Len(t, spec.Values, 1)
	call, ok := spec.Values[0].(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr, got %T", spec.Values[0])
	wrapperIdent, ok := call.Fun.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "wrapper", wrapperIdent.Name)
	require.Len(t, call.Args, 1)
	_, ok = call.Args[0].(*dst.CallExpr)
	require.True(t, ok, "expected inner argument to be a call expression")
}

func TestWrapDeclValue_MultipleValues(t *testing.T) {
	// Simulate: var a, b = val1, val2
	// Go requires len(Values) == len(Names) when initializers are present.
	// Only the initializer belonging to the targeted name is wrapped.
	spec := &dst.ValueSpec{
		Names: []*dst.Ident{{Name: "a"}, {Name: "b"}},
		Values: []dst.Expr{
			&dst.BasicLit{Kind: token.INT, Value: "1"},
			&dst.BasicLit{Kind: token.INT, Value: "2"},
		},
	}

	err := wrapDeclValue(spec, "inc({{ . }})", 1)

	require.NoError(t, err)
	require.Len(t, spec.Values, 2)
	untouched, ok := spec.Values[0].(*dst.BasicLit)
	require.True(t, ok, "expected sibling to stay a literal, got %T", spec.Values[0])
	assert.Equal(t, "1", untouched.Value)
	call, ok := spec.Values[1].(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr, got %T", spec.Values[1])
	fn, ok := call.Fun.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "inc", fn.Name)
}

func TestWrapDeclValue_NoInitializer(t *testing.T) {
	// Simulate: var X int  (no initializer)
	spec := &dst.ValueSpec{
		Names:  []*dst.Ident{{Name: "X"}},
		Values: nil,
	}

	err := wrapDeclValue(spec, "wrapper({{ . }})", 0)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "wrap requires an existing initializer")
}

func TestWrapDeclValue_InvalidTemplate(t *testing.T) {
	spec := &dst.ValueSpec{
		Names:  []*dst.Ident{{Name: "X"}},
		Values: []dst.Expr{&dst.Ident{Name: "x"}},
	}

	err := wrapDeclValue(spec, "func {{ . }}", 0)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to wrap expression")
}

func TestWrapDeclValue_MalformedTemplateSyntax(t *testing.T) {
	spec := &dst.ValueSpec{
		Names:  []*dst.Ident{{Name: "X"}},
		Values: []dst.Expr{&dst.Ident{Name: "x"}},
	}

	err := wrapDeclValue(spec, "{{ unclosed", 0)

	require.Error(t, err)
}

// --- applyDeclRule integration tests ---

// makeVarFile builds a minimal *dst.File containing a single var declaration.
//
//	var <name> int = <initExpr>
//
// Pass initExpr=nil to produce a declaration with no initializer.
func makeVarFile(name string, initExpr dst.Expr) *dst.File {
	spec := &dst.ValueSpec{
		Names: []*dst.Ident{{Name: name}},
		Type:  &dst.Ident{Name: "int"},
	}
	if initExpr != nil {
		spec.Values = []dst.Expr{initExpr}
	}
	return &dst.File{
		Name: &dst.Ident{Name: "main"},
		Decls: []dst.Decl{
			&dst.GenDecl{
				Tok:   token.VAR,
				Specs: []dst.Spec{spec},
			},
		},
	}
}

func TestApplyDeclRule_WrapExpression_Success(t *testing.T) {
	file := makeVarFile("X", &dst.BasicLit{Kind: token.INT, Value: "1"})
	r := &rule.InstDeclRule{
		InstBaseRule: rule.InstBaseRule{Name: "wrap_x"},
		Kind:         "var",
		Identifier:   "X",
		Wrap:         "double({{ . }})",
	}

	err := newTestPhase().applyDeclRule(context.Background(), r, file)

	require.NoError(t, err)
	spec := file.Decls[0].(*dst.GenDecl).Specs[0].(*dst.ValueSpec)
	call, ok := spec.Values[0].(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr after wrap, got %T", spec.Values[0])
	fn, ok := call.Fun.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "double", fn.Name)
}

func TestApplyDeclRule_WrapExpression_DeclarationNotFound(t *testing.T) {
	file := makeVarFile("Y", &dst.BasicLit{Kind: token.INT, Value: "1"})
	r := &rule.InstDeclRule{
		InstBaseRule: rule.InstBaseRule{Name: "wrap_x"},
		Kind:         "var",
		Identifier:   "X", // does not exist in file
		Wrap:         "double({{ . }})",
	}

	err := newTestPhase().applyDeclRule(context.Background(), r, file)

	require.Error(t, err)
	assert.Contains(t, err.Error(), `"X"`)
}

func TestApplyDeclRule_WrapExpression_NoInitializer(t *testing.T) {
	file := makeVarFile("X", nil) // var X int — no initializer
	r := &rule.InstDeclRule{
		InstBaseRule: rule.InstBaseRule{Name: "wrap_x"},
		Kind:         "var",
		Identifier:   "X",
		Wrap:         "double({{ . }})",
	}

	err := newTestPhase().applyDeclRule(context.Background(), r, file)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "wrap requires an existing initializer")
}

func TestApplyDeclRule_WrapExpression_InvalidTemplateSkipsImports(t *testing.T) {
	// Imports is set, but the wrap template fails to compile. The rule must
	// fail before addRuleImports ever runs, so no import spec is added.
	file := makeVarFile("X", &dst.BasicLit{Kind: token.INT, Value: "1"})
	r := &rule.InstDeclRule{
		InstBaseRule: rule.InstBaseRule{
			Name:    "wrap_x",
			Imports: map[string]string{"fmt": "fmt"},
		},
		Kind:       "var",
		Identifier: "X",
		Wrap:       "func {{ . }}",
	}

	err := newTestPhase().applyDeclRule(context.Background(), r, file)

	require.Error(t, err)
	assert.Zero(t, countImportSpecs(file))
}

func TestApplyDeclRule_ReplaceSuccess_AddsImports(t *testing.T) {
	file := makeVarFile("X", &dst.BasicLit{Kind: token.INT, Value: "1"})
	r := &rule.InstDeclRule{
		InstBaseRule: rule.InstBaseRule{
			Name:    "replace_x",
			Imports: map[string]string{"fmt": "fmt"},
		},
		Kind:       "var",
		Identifier: "X",
		Replace:    "99",
	}

	err := newTestPhase().applyDeclRule(context.Background(), r, file)

	require.NoError(t, err)
	assert.Equal(t, 1, countImportSpecs(file), "a successful rewrite must still add the rule's imports")
}

func TestApplyDeclRule_EmptyKind_FunctionTarget(t *testing.T) {
	file := &dst.File{
		Name: &dst.Ident{Name: "main"},
		Decls: []dst.Decl{
			&dst.FuncDecl{
				Name: &dst.Ident{Name: "DefaultHandler"},
				Type: &dst.FuncType{},
			},
		},
	}
	r := &rule.InstDeclRule{
		InstBaseRule: rule.InstBaseRule{
			Name:    "replace_handler",
			Imports: map[string]string{"fmt": "fmt"},
		},
		Kind:       "",
		Identifier: "DefaultHandler",
		Replace:    "CustomHandler",
	}

	err := newTestPhase().applyDeclRule(context.Background(), r, file)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not a var or const declaration")
	assert.Zero(t, countImportSpecs(file), "imports must not be injected when the target isn't a var/const declaration")
}

// --- multi-name ValueSpec tests ---

func parseTestFile(t *testing.T, source string) *dst.File {
	t.Helper()
	file, err := ast.NewAstParser().ParseSource(source)
	require.NoError(t, err)
	return file
}

func TestDeclRuleDoesNotClobberSiblingNames(t *testing.T) {
	file := parseTestFile(t, "package main\n\nvar alpha, beta = 1, 2\n")
	r := &rule.InstDeclRule{
		InstBaseRule: rule.InstBaseRule{Name: "replace_alpha"},
		Kind:         "var",
		Identifier:   "alpha",
		Replace:      "99",
	}

	err := newTestPhase().applyDeclRule(context.Background(), r, file)

	require.NoError(t, err)
	assert.Contains(t, renderFile(t, file), "var alpha, beta = 99, 2")
}

func TestDeclRuleWrapDoesNotClobberSiblingNames(t *testing.T) {
	file := parseTestFile(t, "package main\n\nvar alpha, beta = 1, 2\n")
	r := &rule.InstDeclRule{
		InstBaseRule: rule.InstBaseRule{Name: "wrap_beta"},
		Kind:         "var",
		Identifier:   "beta",
		Wrap:         "double({{ . }})",
	}

	err := newTestPhase().applyDeclRule(context.Background(), r, file)

	require.NoError(t, err)
	assert.Contains(t, renderFile(t, file), "var alpha, beta = 1, double(2)")
}

// A tuple-valued initializer gives two names one value, so no single
// initializer belongs to the targeted name. Both replace and wrap refuse it
// rather than rewrite the declaration into a different shape.
const tupleValuedSource = "package main\n\nfunc pair() (int, int) { return 1, 2 }\n\nvar alpha, beta = pair()\n"

func TestDeclRuleReplaceRejectsTupleValuedInitializer(t *testing.T) {
	file := parseTestFile(t, tupleValuedSource)
	r := &rule.InstDeclRule{
		InstBaseRule: rule.InstBaseRule{Name: "replace_alpha"},
		Kind:         "var",
		Identifier:   "alpha",
		Replace:      "99",
	}

	err := newTestPhase().applyDeclRule(context.Background(), r, file)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "declares 2 names but has 1 initializer(s)")
	assert.Contains(t, renderFile(t, file), "var alpha, beta = pair()", "declaration must be left untouched")
}

func TestDeclRuleReplaceRejectsMultiNameNoInitializer(t *testing.T) {
	file := parseTestFile(t, "package main\n\nvar alpha, beta int\n")
	r := &rule.InstDeclRule{
		InstBaseRule: rule.InstBaseRule{Name: "replace_alpha"},
		Kind:         "var",
		Identifier:   "alpha",
		Replace:      "99",
	}

	err := newTestPhase().applyDeclRule(context.Background(), r, file)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "declares 2 names but has 0 initializer(s)")
	assert.Contains(t, renderFile(t, file), "var alpha, beta int", "declaration must be left untouched")
}

func TestDeclRuleWrapRejectsTupleValuedInitializer(t *testing.T) {
	file := parseTestFile(t, tupleValuedSource)
	r := &rule.InstDeclRule{
		InstBaseRule: rule.InstBaseRule{Name: "wrap_alpha"},
		Kind:         "var",
		Identifier:   "alpha",
		Wrap:         "double({{ . }})",
	}

	err := newTestPhase().applyDeclRule(context.Background(), r, file)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "declares 2 names but has 1 initializer(s)")
	assert.Contains(t, renderFile(t, file), "var alpha, beta = pair()", "declaration must be left untouched")
}

func TestDeclRuleReplaceRejectsTupleValuedInitializer_SkipsImports(t *testing.T) {
	// Imports is set, but the tuple-valued initializer makes replace fail its
	// arity check. The rule must fail before addRuleImports ever runs.
	file := parseTestFile(t, tupleValuedSource)
	r := &rule.InstDeclRule{
		InstBaseRule: rule.InstBaseRule{
			Name:    "replace_alpha",
			Imports: map[string]string{"fmt": "fmt"},
		},
		Kind:       "var",
		Identifier: "alpha",
		Replace:    "99",
	}

	err := newTestPhase().applyDeclRule(context.Background(), r, file)

	require.Error(t, err)
	assert.Zero(t, countImportSpecs(file))
}

func TestDeclRuleReplaceHandlesConstIotaImplicitRepeat(t *testing.T) {
	file := parseTestFile(t, "package main\n\nconst (\n\tA = iota\n\tB\n)\n")
	r := &rule.InstDeclRule{
		InstBaseRule: rule.InstBaseRule{Name: "replace_b"},
		Kind:         "const",
		Identifier:   "B",
		Replace:      "99",
	}

	err := newTestPhase().applyDeclRule(context.Background(), r, file)

	require.NoError(t, err)
	assert.Contains(t, renderFile(t, file), "B = 99")
}

func TestParseValueExpr(t *testing.T) {
	t.Run("valid expression", func(t *testing.T) {
		expr, err := parseValueExpr("123")
		require.NoError(t, err)
		require.NotNil(t, expr)
	})

	t.Run("syntax error in expression", func(t *testing.T) {
		_, err := parseValueExpr("1 +")
		require.Error(t, err)
	})

	t.Run("multiple values expression produces error instead of taking first value", func(t *testing.T) {
		_, err := parseValueExpr("1, 2")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected 1 value, got 2")
	})
}
