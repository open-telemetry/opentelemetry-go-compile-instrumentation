// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTemplate_Success(t *testing.T) {
	text := "wrapper({{ . }})"

	tmpl, err := NewTemplate(text)

	require.NoError(t, err)
	assert.NotNil(t, tmpl)
	assert.Equal(t, text, tmpl.Source)
}

func TestNewTemplate_InvalidSyntax(t *testing.T) {
	text := "wrapper({{ .Field )" // Invalid template syntax - missing closing }}

	tmpl, err := NewTemplate(text)

	require.Error(t, err)
	assert.Nil(t, tmpl)
	assert.Contains(t, err.Error(), "failed to parse template")
}

func TestNewTemplate_EmptyTemplate(t *testing.T) {
	text := ""

	tmpl, err := NewTemplate(text)

	require.NoError(t, err)
	assert.NotNil(t, tmpl)
	assert.Equal(t, text, tmpl.Source)
}

func TestCompileExpression_SimpleWrapping(t *testing.T) {
	tmpl, err := NewTemplate("wrapper({{ . }})")
	require.NoError(t, err)

	// Create a simple call expression: funcCall()
	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "funcCall"},
	}

	result, err := tmpl.CompileExpression(originalCall)

	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify it's a call expression
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr, got %T", result)

	// Verify the outer wrapper function
	wrapperIdent, ok := resultCall.Fun.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "wrapper", wrapperIdent.Name)

	// Verify the original call is inside
	require.Len(t, resultCall.Args, 1)
	innerCall, ok := resultCall.Args[0].(*dst.CallExpr)
	require.True(t, ok)
	innerIdent, ok := innerCall.Fun.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "funcCall", innerIdent.Name)
}

func TestCompileExpression_IIFE(t *testing.T) {
	tmpl, err := NewTemplate("(func() int { return {{ . }} })()")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "getValue"},
	}

	result, err := tmpl.CompileExpression(originalCall)

	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify it's a call expression (the IIFE invocation)
	_, ok := result.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr for IIFE, got %T", result)
}

func TestCompileExpression_MultiplePlaceholders(t *testing.T) {
	// Template with multiple {{ . }} occurrences
	tmpl, err := NewTemplate("combine({{ . }}, {{ . }})")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "getValue"},
	}

	result, err := tmpl.CompileExpression(originalCall)

	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify it's a call expression
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok)

	// Verify both arguments are present
	assert.Len(t, resultCall.Args, 2)
}

func TestCompileExpression_InvalidGoSyntax(t *testing.T) {
	// Template that parses fine but produces invalid Go syntax
	tmpl, err := NewTemplate("func {{ . }}") // "func" keyword without proper syntax
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "test"},
	}

	result, err := tmpl.CompileExpression(originalCall)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to parse generated code")
}

func TestCompileExpression_ComplexNestedExpression(t *testing.T) {
	tmpl, err := NewTemplate("outer(middle({{ . }}))")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "inner"},
	}

	result, err := tmpl.CompileExpression(originalCall)

	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify nested structure: outer(middle(inner()))
	outerCall, ok := result.(*dst.CallExpr)
	require.True(t, ok)
	assert.Equal(t, "outer", outerCall.Fun.(*dst.Ident).Name)

	require.Len(t, outerCall.Args, 1)
	middleCall, ok := outerCall.Args[0].(*dst.CallExpr)
	require.True(t, ok)
	assert.Equal(t, "middle", middleCall.Fun.(*dst.Ident).Name)

	require.Len(t, middleCall.Args, 1)
	innerCall, ok := middleCall.Args[0].(*dst.CallExpr)
	require.True(t, ok)
	assert.Equal(t, "inner", innerCall.Fun.(*dst.Ident).Name)
}

func TestCompileExpression_WithBinaryExpression(t *testing.T) {
	tmpl, err := NewTemplate("{{ . }} + 1")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "getValue"},
	}

	result, err := tmpl.CompileExpression(originalCall)

	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify it's a binary expression
	binaryExpr, ok := result.(*dst.BinaryExpr)
	require.True(t, ok, "expected *dst.BinaryExpr, got %T", result)

	// Verify the left side is our call
	leftCall, ok := binaryExpr.X.(*dst.CallExpr)
	require.True(t, ok)
	assert.Equal(t, "getValue", leftCall.Fun.(*dst.Ident).Name)
}

func TestCompileExpression_SelectorExpression(t *testing.T) {
	tmpl, err := NewTemplate("{{ . }}.Field")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "getStruct"},
	}

	result, err := tmpl.CompileExpression(originalCall)

	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify it's a selector expression
	selExpr, ok := result.(*dst.SelectorExpr)
	require.True(t, ok, "expected *dst.SelectorExpr, got %T", result)
	assert.Equal(t, "Field", selExpr.Sel.Name)

	// Verify X is our call
	call, ok := selExpr.X.(*dst.CallExpr)
	require.True(t, ok)
	assert.Equal(t, "getStruct", call.Fun.(*dst.Ident).Name)
}

func TestCompileExpression_EmptyResult(t *testing.T) {
	// Template that produces nothing (empty expression)
	tmpl, err := NewTemplate("")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "test"},
	}

	result, err := tmpl.CompileExpression(originalCall)

	// Should error because the function body is empty
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "function body is empty")
}

func TestCompileExpression_PlaceholderNotReplaced(t *testing.T) {
	tmpl, err := NewTemplate(`wrapper("{{ . }}")`)
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "test"},
	}

	result, err := tmpl.CompileExpression(originalCall)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "placeholder")
}

func TestCompileExpression_MultipleStatements(t *testing.T) {
	tmpl, err := NewTemplate("first(); {{ . }}")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "test"},
	}

	result, err := tmpl.CompileExpression(originalCall)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "single expression statement")
}

func TestCompileExpression_NonExpressionStatement(t *testing.T) {
	// Template that produces a non-expression statement
	// This is tricky - we need something that parses as a statement but not as an expression
	tmpl, err := NewTemplate("return")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "test"},
	}

	result, err := tmpl.CompileExpression(originalCall)

	// Should error because it's not an expression statement
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "expected expression statement")
}
