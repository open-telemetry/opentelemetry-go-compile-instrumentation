// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// typeParamsT builds a type-parameter list with a single parameter named "T".
func typeParamsT() *dst.FieldList {
	return &dst.FieldList{
		List: []*dst.Field{{Names: []*dst.Ident{dst.NewIdent("T")}}},
	}
}

func TestIsTypeParameter(t *testing.T) {
	tp := typeParamsT()
	assert.True(t, isTypeParameter(dst.NewIdent("T"), tp))
	assert.False(t, isTypeParameter(dst.NewIdent("string"), tp))
	assert.False(t, isTypeParameter(dst.NewIdent("T"), nil))
	// Non-identifier expressions are never type parameters.
	assert.False(t, isTypeParameter(&dst.StarExpr{X: dst.NewIdent("T")}, tp))
}

func TestReplaceTypeParamsWithAny(t *testing.T) {
	tp := typeParamsT()

	t.Run("bare type parameter becomes interface{}", func(t *testing.T) {
		got := replaceTypeParamsWithAny(dst.NewIdent("T"), tp)
		assert.IsType(t, &dst.InterfaceType{}, got)
	})

	t.Run("pointer to type parameter", func(t *testing.T) {
		got := replaceTypeParamsWithAny(&dst.StarExpr{X: dst.NewIdent("T")}, tp)
		star, ok := got.(*dst.StarExpr)
		require.True(t, ok)
		assert.IsType(t, &dst.InterfaceType{}, star.X)
	})

	t.Run("slice of type parameter", func(t *testing.T) {
		got := replaceTypeParamsWithAny(&dst.ArrayType{Elt: dst.NewIdent("T")}, tp)
		arr, ok := got.(*dst.ArrayType)
		require.True(t, ok)
		assert.IsType(t, &dst.InterfaceType{}, arr.Elt)
	})

	t.Run("map with type parameter key and value", func(t *testing.T) {
		got := replaceTypeParamsWithAny(&dst.MapType{Key: dst.NewIdent("T"), Value: dst.NewIdent("T")}, tp)
		m, ok := got.(*dst.MapType)
		require.True(t, ok)
		assert.IsType(t, &dst.InterfaceType{}, m.Key)
		assert.IsType(t, &dst.InterfaceType{}, m.Value)
	})

	t.Run("channel of type parameter", func(t *testing.T) {
		got := replaceTypeParamsWithAny(&dst.ChanType{Dir: dst.SEND, Value: dst.NewIdent("T")}, tp)
		ch, ok := got.(*dst.ChanType)
		require.True(t, ok)
		assert.Equal(t, dst.SEND, ch.Dir)
		assert.IsType(t, &dst.InterfaceType{}, ch.Value)
	})

	t.Run("generic index expression becomes interface{}", func(t *testing.T) {
		got := replaceTypeParamsWithAny(&dst.IndexExpr{X: dst.NewIdent("GenStruct"), Index: dst.NewIdent("T")}, tp)
		assert.IsType(t, &dst.InterfaceType{}, got)
	})

	t.Run("generic index list expression becomes interface{}", func(t *testing.T) {
		got := replaceTypeParamsWithAny(
			&dst.IndexListExpr{X: dst.NewIdent("GenStruct"), Indices: []dst.Expr{dst.NewIdent("T"), dst.NewIdent("U")}},
			tp,
		)
		assert.IsType(t, &dst.InterfaceType{}, got)
	})

	t.Run("variadic type parameter preserves ellipsis", func(t *testing.T) {
		got := replaceTypeParamsWithAny(&dst.Ellipsis{Elt: dst.NewIdent("T")}, tp)
		ell, ok := got.(*dst.Ellipsis)
		require.True(t, ok)
		assert.IsType(t, &dst.InterfaceType{}, ell.Elt)
	})

	t.Run("func type processes params and results", func(t *testing.T) {
		fn := &dst.FuncType{
			Params: &dst.FieldList{List: []*dst.Field{
				{Names: []*dst.Ident{dst.NewIdent("x")}, Type: dst.NewIdent("T")},
			}},
			Results: &dst.FieldList{List: []*dst.Field{
				{Type: dst.NewIdent("T")},
			}},
		}
		got := replaceTypeParamsWithAny(fn, tp)
		newFn, ok := got.(*dst.FuncType)
		require.True(t, ok)
		require.Len(t, newFn.Params.List, 1)
		require.Len(t, newFn.Results.List, 1)
		// The named parameter keeps its name but its type becomes interface{}.
		require.Len(t, newFn.Params.List[0].Names, 1)
		assert.Equal(t, "x", newFn.Params.List[0].Names[0].Name)
		assert.IsType(t, &dst.InterfaceType{}, newFn.Params.List[0].Type)
		assert.IsType(t, &dst.InterfaceType{}, newFn.Results.List[0].Type)
	})

	t.Run("non-type-param identifier is returned unchanged", func(t *testing.T) {
		in := dst.NewIdent("string")
		got := replaceTypeParamsWithAny(in, tp)
		assert.Same(t, in, got)
	})

	t.Run("selector expression is returned unchanged", func(t *testing.T) {
		in := &dst.SelectorExpr{X: dst.NewIdent("pkg"), Sel: dst.NewIdent("Type")}
		got := replaceTypeParamsWithAny(in, tp)
		assert.Same(t, in, got)
	})
}
