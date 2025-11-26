// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertSimpleCall(t *testing.T, expr *dst.CallExpr, expectedFuncName string, expectedArgCount int) {
	funcIdent, _ := expr.Fun.(*dst.Ident)
	assert.Equal(t, expectedFuncName, funcIdent.Name)
	assert.Len(t, expr.Args, expectedArgCount)
}

func assertIndexExprCall(
	t *testing.T,
	expr *dst.CallExpr,
	expectedFuncName string,
	expectedTypeParam string,
	expectedArgCount int,
) {
	indexExpr, _ := expr.Fun.(*dst.IndexExpr)
	funcIdent, _ := indexExpr.X.(*dst.Ident)
	assert.Equal(t, expectedFuncName, funcIdent.Name)
	typeIdent, _ := indexExpr.Index.(*dst.Ident)
	assert.Equal(t, expectedTypeParam, typeIdent.Name)
	assert.Len(t, expr.Args, expectedArgCount)
}

func assertIndexListExprCall(
	t *testing.T,
	expr *dst.CallExpr,
	expectedFuncName string,
	expectedTypeParams []string,
	expectedArgCount int,
) {
	indexListExpr, _ := expr.Fun.(*dst.IndexListExpr)
	funcIdent, _ := indexListExpr.X.(*dst.Ident)
	assert.Equal(t, expectedFuncName, funcIdent.Name)
	require.Len(t, indexListExpr.Indices, len(expectedTypeParams))
	for i, expectedParam := range expectedTypeParams {
		paramIdent, _ := indexListExpr.Indices[i].(*dst.Ident)
		assert.Equal(t, expectedParam, paramIdent.Name)
	}
	assert.Len(t, expr.Args, expectedArgCount)
}

// Helper function to parse a complete function and extract its type parameters
func parseFuncTypeParams(t *testing.T, funcSource string) *dst.FieldList {
	parser := NewAstParser()
	file, err := parser.ParseSource("package main\n" + funcSource)
	require.NoError(t, err)
	require.Len(t, file.Decls, 1)
	funcDecl, ok := file.Decls[0].(*dst.FuncDecl)
	require.True(t, ok)
	return funcDecl.Type.TypeParams
}

func TestCallTo(t *testing.T) {
	tests := []struct {
		name       string
		funcName   string
		funcSource string // Source code for parsing type params
		args       []dst.Expr
		validate   func(*testing.T, *dst.CallExpr)
	}{
		{
			name:       "nil type params returns simple call",
			funcName:   "Foo",
			funcSource: "func Foo(x, y int) {}", // No type params
			args:       []dst.Expr{Ident("x"), Ident("y")},
			validate: func(t *testing.T, expr *dst.CallExpr) {
				assertSimpleCall(t, expr, "Foo", 2)
			},
		},
		{
			name:       "single type parameter creates IndexExpr",
			funcName:   "GenericFunc",
			funcSource: "func GenericFunc[T any](value T) {}",
			args:       []dst.Expr{Ident("value")},
			validate: func(t *testing.T, expr *dst.CallExpr) {
				assertIndexExprCall(t, expr, "GenericFunc", "T", 1)
			},
		},
		{
			name:       "multiple type parameters creates IndexListExpr",
			funcName:   "MultiGeneric",
			funcSource: "func MultiGeneric[T any, U comparable](x T, y U) {}",
			args:       []dst.Expr{Ident("x"), Ident("y")},
			validate: func(t *testing.T, expr *dst.CallExpr) {
				assertIndexListExprCall(t, expr, "MultiGeneric", []string{"T", "U"}, 2)
			},
		},
		{
			name:       "field with multiple names creates multiple indices",
			funcName:   "MultiNameGeneric",
			funcSource: "func MultiNameGeneric[T, U any](value T) {}",
			args:       []dst.Expr{Ident("value")},
			validate: func(t *testing.T, expr *dst.CallExpr) {
				assertIndexListExprCall(t, expr, "MultiNameGeneric", []string{"T", "U"}, 1)
			},
		},
		{
			name:       "no arguments with type parameters",
			funcName:   "NoArgsGeneric",
			funcSource: "func NoArgsGeneric[T any]() {}",
			args:       []dst.Expr{},
			validate: func(t *testing.T, expr *dst.CallExpr) {
				assertIndexExprCall(t, expr, "NoArgsGeneric", "T", 0)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			typeParams := parseFuncTypeParams(t, tt.funcSource)
			result := CallTo(tt.funcName, typeParams, tt.args)
			require.NotNil(t, result)
			tt.validate(t, result)
		})
	}
}

func TestSplitMultiNameFields(t *testing.T) {
	t.Run("nil input returns nil", func(t *testing.T) {
		assert.Nil(t, SplitMultiNameFields(nil))
	})

	t.Run("empty field list returns empty list", func(t *testing.T) {
		input := &dst.FieldList{List: []*dst.Field{}}
		result := SplitMultiNameFields(input)
		assert.NotNil(t, result)
		assert.Empty(t, result.List)
	})

	t.Run("single name fields remain unchanged", func(t *testing.T) {
		input := &dst.FieldList{
			List: []*dst.Field{
				{Names: []*dst.Ident{Ident("a")}, Type: Ident("int")},
				{Names: []*dst.Ident{Ident("b")}, Type: Ident("string")},
			},
		}
		result := SplitMultiNameFields(input)
		require.Len(t, result.List, 2)
		assert.Equal(t, "a", result.List[0].Names[0].Name)
		assert.Equal(t, "int", result.List[0].Type.(*dst.Ident).Name)
		assert.Equal(t, "b", result.List[1].Names[0].Name)
		assert.Equal(t, "string", result.List[1].Type.(*dst.Ident).Name)
	})

	t.Run("multi-name field is split into separate fields", func(t *testing.T) {
		input := &dst.FieldList{
			List: []*dst.Field{
				{
					Names: []*dst.Ident{Ident("a"), Ident("b")},
					Type:  Ident("int"),
				},
			},
		}
		result := SplitMultiNameFields(input)
		require.Len(t, result.List, 2)
		assert.Equal(t, "a", result.List[0].Names[0].Name)
		assert.Equal(t, "int", result.List[0].Type.(*dst.Ident).Name)
		assert.Equal(t, "b", result.List[1].Names[0].Name)
		assert.Equal(t, "int", result.List[1].Type.(*dst.Ident).Name)
	})

	t.Run("underscore parameters are properly split", func(t *testing.T) {
		input := &dst.FieldList{
			List: []*dst.Field{
				{
					Names: []*dst.Ident{Ident("_"), Ident("_")},
					Type:  InterfaceType(),
				},
			},
		}
		result := SplitMultiNameFields(input)
		require.Len(t, result.List, 2)
		assert.Equal(t, "_", result.List[0].Names[0].Name)
		assert.NotNil(t, result.List[0].Type.(*dst.InterfaceType))
		assert.Equal(t, "_", result.List[1].Names[0].Name)
		assert.NotNil(t, result.List[1].Type.(*dst.InterfaceType))
	})

	t.Run("mixed single and multi-name fields", func(t *testing.T) {
		input := &dst.FieldList{
			List: []*dst.Field{
				{Names: []*dst.Ident{Ident("a")}, Type: Ident("int")},
				{Names: []*dst.Ident{Ident("b"), Ident("c")}, Type: Ident("string")},
				{Names: []*dst.Ident{Ident("d")}, Type: Ident("bool")},
			},
		}
		result := SplitMultiNameFields(input)
		require.Len(t, result.List, 4)
		assert.Equal(t, "a", result.List[0].Names[0].Name)
		assert.Equal(t, "int", result.List[0].Type.(*dst.Ident).Name)
		assert.Equal(t, "b", result.List[1].Names[0].Name)
		assert.Equal(t, "string", result.List[1].Type.(*dst.Ident).Name)
		assert.Equal(t, "c", result.List[2].Names[0].Name)
		assert.Equal(t, "string", result.List[2].Type.(*dst.Ident).Name)
		assert.Equal(t, "d", result.List[3].Names[0].Name)
		assert.Equal(t, "bool", result.List[3].Type.(*dst.Ident).Name)
	})

	t.Run("unnamed field remains unchanged", func(t *testing.T) {
		input := &dst.FieldList{
			List: []*dst.Field{
				{Names: nil, Type: Ident("int")},
			},
		}
		result := SplitMultiNameFields(input)
		require.Len(t, result.List, 1)
		assert.Nil(t, result.List[0].Names)
		assert.Equal(t, "int", result.List[0].Type.(*dst.Ident).Name)
	})

	t.Run("modifications to result don't affect original", func(t *testing.T) {
		original := &dst.FieldList{
			List: []*dst.Field{
				{Names: []*dst.Ident{Ident("a"), Ident("b")}, Type: Ident("int")},
			},
		}
		result := SplitMultiNameFields(original)

		result.List[0].Names[0].Name = "Modified"

		assert.Equal(t, "a", original.List[0].Names[0].Name)
		assert.Equal(t, "Modified", result.List[0].Names[0].Name)
	})
}
