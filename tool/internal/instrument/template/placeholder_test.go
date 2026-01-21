// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2023-present Datadog, Inc.

package template

import (
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	result := replacePlaceholder(astWithPlaceholder, replacement)

	// Verify
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok)
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

	result := replacePlaceholder(astWithPlaceholders, replacement)

	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok)
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

	result := replacePlaceholder(astWithoutPlaceholder, replacement)

	// Verify AST is unchanged
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok)
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

	result := replacePlaceholder(astWithNested, replacement)

	// Navigate to the nested location
	outerCall, ok := result.(*dst.CallExpr)
	require.True(t, ok)

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

	result := replacePlaceholder(astWithWrongPrefix, replacement)

	// Verify not replaced
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok)

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

	result := replacePlaceholder(astWithWrongName, replacement)

	// Verify not replaced
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok)

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

	result := replacePlaceholder(astComplex, replacement)

	// Verify structure
	binaryExpr, ok := result.(*dst.BinaryExpr)
	require.True(t, ok)

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

	result := replacePlaceholder(astWithLiteral, replacement)

	// Verify unchanged
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok)
	require.Len(t, resultCall.Args, 2)

	lit, ok := resultCall.Args[0].(*dst.BasicLit)
	require.True(t, ok)
	assert.Equal(t, "\"string\"", lit.Value)

	ident, ok := resultCall.Args[1].(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "ident", ident.Name)
}
