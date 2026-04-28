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

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
)

// newTestPhase returns a minimal InstrumentPhase suitable for unit tests that
// do not exercise import injection or compilation (logger discards all output).
func newTestPhase() *InstrumentPhase {
	return &InstrumentPhase{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// --- wrapDeclValues helper tests ---

func TestWrapDeclValues_Success(t *testing.T) {
	// Simulate: var X = someCall()
	spec := &dst.ValueSpec{
		Names: []*dst.Ident{{Name: "X"}},
		Values: []dst.Expr{
			&dst.CallExpr{Fun: &dst.Ident{Name: "someCall"}},
		},
	}

	err := wrapDeclValues(spec, "wrapper({{ . }})")

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

func TestWrapDeclValues_MultipleValues(t *testing.T) {
	// Simulate: var a, b = val1, val2
	// Go requires len(Values) == len(Names) when initializers are present.
	// Each value is wrapped independently.
	spec := &dst.ValueSpec{
		Names: []*dst.Ident{{Name: "a"}, {Name: "b"}},
		Values: []dst.Expr{
			&dst.BasicLit{Kind: token.INT, Value: "1"},
			&dst.BasicLit{Kind: token.INT, Value: "2"},
		},
	}

	err := wrapDeclValues(spec, "inc({{ . }})")

	require.NoError(t, err)
	require.Len(t, spec.Values, 2)
	for i, v := range spec.Values {
		call, ok := v.(*dst.CallExpr)
		require.True(t, ok, "index %d: expected *dst.CallExpr, got %T", i, v)
		fn, ok := call.Fun.(*dst.Ident)
		require.True(t, ok)
		assert.Equal(t, "inc", fn.Name)
	}
}

func TestWrapDeclValues_NoInitializer(t *testing.T) {
	// Simulate: var X int  (no initializer)
	spec := &dst.ValueSpec{
		Names:  []*dst.Ident{{Name: "X"}},
		Values: nil,
	}

	err := wrapDeclValues(spec, "wrapper({{ . }})")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "wrap requires an existing initializer")
}

func TestWrapDeclValues_InvalidTemplate(t *testing.T) {
	spec := &dst.ValueSpec{
		Names:  []*dst.Ident{{Name: "X"}},
		Values: []dst.Expr{&dst.Ident{Name: "x"}},
	}

	err := wrapDeclValues(spec, "func {{ . }}")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to wrap expression")
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
