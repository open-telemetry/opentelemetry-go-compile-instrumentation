// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCallTemplate_Success(t *testing.T) {
	text := "wrapper({{ . }})"

	tmpl, err := newCallTemplate(text)

	require.NoError(t, err)
	assert.NotNil(t, tmpl)
	assert.Equal(t, text, tmpl.String())
}

func TestNewCallTemplate_InvalidSyntax(t *testing.T) {
	text := "wrapper({{ .Field )" // Invalid template syntax - missing closing }}

	tmpl, err := newCallTemplate(text)

	require.Error(t, err)
	assert.Nil(t, tmpl)
	assert.Contains(t, err.Error(), "failed to parse template")
}

func TestNewCallTemplate_EmptyTemplate(t *testing.T) {
	text := ""

	tmpl, err := newCallTemplate(text)

	require.NoError(t, err)
	assert.NotNil(t, tmpl)
	assert.Equal(t, text, tmpl.String())
}

func TestCompileExpression_SimpleWrapping(t *testing.T) {
	tmpl, err := newCallTemplate("wrapper({{ . }})")
	require.NoError(t, err)

	// Create a simple call expression: funcCall()
	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "funcCall"},
	}

	result, err := tmpl.compileExpression(originalCall)

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
	tmpl, err := newCallTemplate("(func() int { return {{ . }} })()")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "getValue"},
	}

	result, err := tmpl.compileExpression(originalCall)

	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify it's a call expression (the IIFE invocation)
	_, ok := result.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr for IIFE, got %T", result)
}

func TestCompileExpression_MultiplePlaceholders(t *testing.T) {
	// Template with multiple {{ . }} occurrences
	tmpl, err := newCallTemplate("combine({{ . }}, {{ . }})")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "getValue"},
	}

	result, err := tmpl.compileExpression(originalCall)

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
	tmpl, err := newCallTemplate("func {{ . }}") // "func" keyword without proper syntax
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "test"},
	}

	result, err := tmpl.compileExpression(originalCall)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to parse generated code")
}

func TestCompileExpression_ComplexNestedExpression(t *testing.T) {
	tmpl, err := newCallTemplate("outer(middle({{ . }}))")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "inner"},
	}

	result, err := tmpl.compileExpression(originalCall)

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
	tmpl, err := newCallTemplate("{{ . }} + 1")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "getValue"},
	}

	result, err := tmpl.compileExpression(originalCall)

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
	tmpl, err := newCallTemplate("{{ . }}.Field")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "getStruct"},
	}

	result, err := tmpl.compileExpression(originalCall)

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
	tmpl, err := newCallTemplate("")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "test"},
	}

	result, err := tmpl.compileExpression(originalCall)

	// Should error because the function body is empty
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "function body is empty")
}

func TestCompileExpression_PlaceholderNotReplaced(t *testing.T) {
	tmpl, err := newCallTemplate(`wrapper("{{ . }}")`)
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "test"},
	}

	result, err := tmpl.compileExpression(originalCall)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "placeholder")
}

func TestCompileExpression_MultipleStatements(t *testing.T) {
	tmpl, err := newCallTemplate("first(); {{ . }}")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "test"},
	}

	result, err := tmpl.compileExpression(originalCall)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "single expression statement")
}

func TestCompileExpression_NonExpressionStatement(t *testing.T) {
	// Template that produces a non-expression statement
	// This is tricky - we need something that parses as a statement but not as an expression
	tmpl, err := newCallTemplate("return")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "test"},
	}

	result, err := tmpl.compileExpression(originalCall)

	// Should error because it's not an expression statement
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "expected expression statement")
}

func TestReplacePlaceholder_SingleOccurrence(t *testing.T) {
	// Create AST with _.PLACEHOLDER_0
	astWithPlaceholder := &dst.CallExpr{
		Fun: &dst.Ident{Name: "wrapper"},
		Args: []dst.Expr{
			&dst.SelectorExpr{
				X:   &dst.Ident{Name: "_"},
				Sel: &dst.Ident{Name: "PLACEHOLDER_0"},
			},
		},
	}

	// Create replacement node
	replacement := &dst.CallExpr{
		Fun: &dst.Ident{Name: "originalCall"},
	}

	// Replace
	result, replaced := replacePlaceholder(astWithPlaceholder, replacement)

	// Verify
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok)
	assert.True(t, replaced)
	assert.Equal(t, "wrapper", resultCall.Fun.(*dst.Ident).Name)

	require.Len(t, resultCall.Args, 1)
	replacedCall, ok := resultCall.Args[0].(*dst.CallExpr)
	require.True(t, ok)
	assert.Equal(t, "originalCall", replacedCall.Fun.(*dst.Ident).Name)
}

func TestReplacePlaceholder_MultipleOccurrences(t *testing.T) {
	// Create AST with two _.PLACEHOLDER_0 occurrences
	astWithPlaceholders := &dst.CallExpr{
		Fun: &dst.Ident{Name: "combine"},
		Args: []dst.Expr{
			&dst.SelectorExpr{
				X:   &dst.Ident{Name: "_"},
				Sel: &dst.Ident{Name: "PLACEHOLDER_0"},
			},
			&dst.SelectorExpr{
				X:   &dst.Ident{Name: "_"},
				Sel: &dst.Ident{Name: "PLACEHOLDER_0"},
			},
		},
	}

	replacement := &dst.Ident{Name: "value"}

	result, replaced := replacePlaceholder(astWithPlaceholders, replacement)

	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok)
	assert.True(t, replaced)
	require.Len(t, resultCall.Args, 2)

	// Both should be replaced
	arg1, ok := resultCall.Args[0].(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "value", arg1.Name)

	arg2, ok := resultCall.Args[1].(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "value", arg2.Name)
}

func TestReplacePlaceholder_NoPlaceholders(t *testing.T) {
	// Create AST without placeholders
	astWithoutPlaceholder := &dst.CallExpr{
		Fun: &dst.Ident{Name: "simpleCall"},
		Args: []dst.Expr{
			&dst.Ident{Name: "arg1"},
		},
	}

	replacement := &dst.Ident{Name: "shouldNotAppear"}

	result, replaced := replacePlaceholder(astWithoutPlaceholder, replacement)

	// Verify AST is unchanged
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok)
	assert.False(t, replaced)
	assert.Equal(t, "simpleCall", resultCall.Fun.(*dst.Ident).Name)

	require.Len(t, resultCall.Args, 1)
	arg, ok := resultCall.Args[0].(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "arg1", arg.Name)
}

func TestReplacePlaceholder_NestedStructure(t *testing.T) {
	// Create nested AST with placeholder deep inside
	astWithNested := &dst.CallExpr{
		Fun: &dst.Ident{Name: "outer"},
		Args: []dst.Expr{
			&dst.CallExpr{
				Fun: &dst.Ident{Name: "middle"},
				Args: []dst.Expr{
					&dst.SelectorExpr{
						X:   &dst.Ident{Name: "_"},
						Sel: &dst.Ident{Name: "PLACEHOLDER_0"},
					},
				},
			},
		},
	}

	replacement := &dst.Ident{Name: "innerValue"}

	result, replaced := replacePlaceholder(astWithNested, replacement)

	// Navigate to the nested location
	outerCall, ok := result.(*dst.CallExpr)
	require.True(t, ok)
	assert.True(t, replaced)

	middleCall, ok := outerCall.Args[0].(*dst.CallExpr)
	require.True(t, ok)

	innerValue, ok := middleCall.Args[0].(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "innerValue", innerValue.Name)
}

func TestReplacePlaceholder_WrongSelectorPrefix(t *testing.T) {
	// Create selector that looks like placeholder but has wrong prefix (x.PLACEHOLDER_0)
	astWithWrongPrefix := &dst.CallExpr{
		Fun: &dst.Ident{Name: "wrapper"},
		Args: []dst.Expr{
			&dst.SelectorExpr{
				X:   &dst.Ident{Name: "x"}, // Not "_"
				Sel: &dst.Ident{Name: "PLACEHOLDER_0"},
			},
		},
	}

	replacement := &dst.Ident{Name: "shouldNotReplace"}

	result, replaced := replacePlaceholder(astWithWrongPrefix, replacement)

	// Verify not replaced
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok)
	assert.False(t, replaced)

	selector, ok := resultCall.Args[0].(*dst.SelectorExpr)
	require.True(t, ok)
	assert.Equal(t, "x", selector.X.(*dst.Ident).Name)
	assert.Equal(t, "PLACEHOLDER_0", selector.Sel.Name)
}

func TestReplacePlaceholder_WrongSelectorName(t *testing.T) {
	// Create selector with right prefix but wrong name (_.OTHER)
	astWithWrongName := &dst.CallExpr{
		Fun: &dst.Ident{Name: "wrapper"},
		Args: []dst.Expr{
			&dst.SelectorExpr{
				X:   &dst.Ident{Name: "_"},
				Sel: &dst.Ident{Name: "OTHER"}, // Not "PLACEHOLDER_0"
			},
		},
	}

	replacement := &dst.Ident{Name: "shouldNotReplace"}

	result, replaced := replacePlaceholder(astWithWrongName, replacement)

	// Verify not replaced
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok)
	assert.False(t, replaced)

	selector, ok := resultCall.Args[0].(*dst.SelectorExpr)
	require.True(t, ok)
	assert.Equal(t, "_", selector.X.(*dst.Ident).Name)
	assert.Equal(t, "OTHER", selector.Sel.Name)
}

func TestReplacePlaceholder_ComplexAST(t *testing.T) {
	// Create complex AST with binary expressions, function calls, etc.
	astComplex := &dst.BinaryExpr{
		Op: 0, // placeholder for operator
		X: &dst.CallExpr{
			Fun: &dst.Ident{Name: "left"},
			Args: []dst.Expr{
				&dst.SelectorExpr{
					X:   &dst.Ident{Name: "_"},
					Sel: &dst.Ident{Name: "PLACEHOLDER_0"},
				},
			},
		},
		Y: &dst.CallExpr{
			Fun: &dst.Ident{Name: "right"},
			Args: []dst.Expr{
				&dst.BasicLit{Value: "42"},
			},
		},
	}

	replacement := &dst.Ident{Name: "replacedValue"}

	result, replaced := replacePlaceholder(astComplex, replacement)

	// Verify structure
	binaryExpr, ok := result.(*dst.BinaryExpr)
	require.True(t, ok)
	assert.True(t, replaced)

	leftCall, ok := binaryExpr.X.(*dst.CallExpr)
	require.True(t, ok)

	replacedIdent, ok := leftCall.Args[0].(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "replacedValue", replacedIdent.Name)

	// Right side should be unchanged
	rightCall, ok := binaryExpr.Y.(*dst.CallExpr)
	require.True(t, ok)
	assert.Equal(t, "right", rightCall.Fun.(*dst.Ident).Name)
}

func TestReplacePlaceholder_NonSelectorNode(t *testing.T) {
	// Create AST with non-selector nodes (should be ignored by replacer)
	astWithLiteral := &dst.CallExpr{
		Fun: &dst.Ident{Name: "wrapper"},
		Args: []dst.Expr{
			&dst.BasicLit{Value: "\"string\""},
			&dst.Ident{Name: "ident"},
		},
	}

	replacement := &dst.Ident{Name: "shouldNotAppear"}

	result, replaced := replacePlaceholder(astWithLiteral, replacement)

	// Verify unchanged
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok)
	assert.False(t, replaced)
	require.Len(t, resultCall.Args, 2)

	lit, ok := resultCall.Args[0].(*dst.BasicLit)
	require.True(t, ok)
	assert.Equal(t, "\"string\"", lit.Value)

	ident, ok := resultCall.Args[1].(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "ident", ident.Name)
}

func TestCompileExpression_UnknownTemplateTag(t *testing.T) {
	tmpl, err := newCallTemplate("wrapper({{ something }})")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "funcCall"},
	}

	result, err := tmpl.compileExpression(originalCall)
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestParseGoExpression_NonExpressionStatement(t *testing.T) {
	_, err := parseGoExpression("return")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not parse as an expression statement")
}

func TestParseGoExpression_EmptyBody(t *testing.T) {
	_, err := parseGoExpression("")
	require.Error(t, err)
}

func TestParseGoTypeExpression_NoType(t *testing.T) {
	// "var _ = 1" has no explicit type on the value spec, so the parsed shape
	// must be rejected rather than silently producing a nil type.
	_, err := parseGoTypeExpression("= 1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected spec shape")
}
