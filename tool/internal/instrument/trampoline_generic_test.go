// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
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

func TestContainsTypeParameter(t *testing.T) {
	tp := typeParamsT()

	// Real nested cases must work
	assert.True(t, containsTypeParameter(dst.NewIdent("T"), tp))
	assert.True(t, containsTypeParameter(&dst.StarExpr{X: dst.NewIdent("T")}, tp))
	assert.True(t, containsTypeParameter(&dst.ArrayType{Elt: dst.NewIdent("T")}, tp))
	assert.True(t, containsTypeParameter(&dst.MapType{Key: dst.NewIdent("string"), Value: dst.NewIdent("T")}, tp))

	// Non-matching identifiers
	assert.False(t, containsTypeParameter(dst.NewIdent("string"), tp))
	assert.False(t, containsTypeParameter(dst.NewIdent("T"), nil))

	// False-positive risks must return false
	assert.False(t,
		containsTypeParameter(
			&dst.SelectorExpr{
				X:   dst.NewIdent("pkg"),
				Sel: dst.NewIdent("T"),
			}, tp),
		"selector name pkg.T should not match")

	funcType := &dst.FuncType{
		Params: &dst.FieldList{List: []*dst.Field{
			{Names: []*dst.Ident{dst.NewIdent("T")}, Type: dst.NewIdent("int")},
		}},
	}
	assert.False(t, containsTypeParameter(funcType, tp), "parameter named T should not match")

	// Anonymous composite types containing type parameters
	assert.True(t, containsTypeParameter(&dst.ParenExpr{X: dst.NewIdent("T")}, tp), "parenthesized type (T) should match")

	structType := &dst.StructType{
		Fields: &dst.FieldList{List: []*dst.Field{
			{Names: []*dst.Ident{dst.NewIdent("X")}, Type: dst.NewIdent("T")},
		}},
	}
	assert.True(t, containsTypeParameter(structType, tp), "struct{ X T } should match")

	interfaceType := &dst.InterfaceType{
		Methods: &dst.FieldList{List: []*dst.Field{
			{Names: []*dst.Ident{dst.NewIdent("M")}, Type: &dst.FuncType{
				Params: &dst.FieldList{List: []*dst.Field{
					{Type: dst.NewIdent("T")},
				}},
			}},
		}},
	}
	assert.True(t, containsTypeParameter(interfaceType, tp), "interface{ M(T) } should match")
}

func TestReplaceTypeParamsWithAny(t *testing.T) {
	tp := typeParamsT()

	t.Run("bare type parameter becomes interface{}", func(t *testing.T) {
		got := replaceTypeParamsWithAny(dst.NewIdent("T"), tp)
		assert.IsType(t, &dst.InterfaceType{}, got)
	})

	t.Run("pointer to type parameter becomes interface{}", func(t *testing.T) {
		got := replaceTypeParamsWithAny(&dst.StarExpr{X: dst.NewIdent("T")}, tp)
		assert.IsType(t, &dst.InterfaceType{}, got)
	})

	t.Run("slice of type parameter becomes interface{}", func(t *testing.T) {
		got := replaceTypeParamsWithAny(&dst.ArrayType{Elt: dst.NewIdent("T")}, tp)
		assert.IsType(t, &dst.InterfaceType{}, got)
	})

	t.Run("map with type parameter key and value becomes interface{}", func(t *testing.T) {
		got := replaceTypeParamsWithAny(&dst.MapType{Key: dst.NewIdent("T"), Value: dst.NewIdent("T")}, tp)
		assert.IsType(t, &dst.InterfaceType{}, got)
	})

	t.Run("channel of type parameter becomes interface{}", func(t *testing.T) {
		got := replaceTypeParamsWithAny(&dst.ChanType{Dir: dst.SEND, Value: dst.NewIdent("T")}, tp)
		assert.IsType(t, &dst.InterfaceType{}, got)
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

	t.Run("variadic type parameter becomes interface{}", func(t *testing.T) {
		got := replaceTypeParamsWithAny(&dst.Ellipsis{Elt: dst.NewIdent("T")}, tp)
		assert.IsType(t, &dst.InterfaceType{}, got)
	})

	t.Run("func type with type parameter becomes interface{}", func(t *testing.T) {
		fn := &dst.FuncType{
			Params: &dst.FieldList{List: []*dst.Field{
				{Names: []*dst.Ident{dst.NewIdent("x")}, Type: dst.NewIdent("T")},
			}},
			Results: &dst.FieldList{List: []*dst.Field{
				{Type: dst.NewIdent("T")},
			}},
		}
		got := replaceTypeParamsWithAny(fn, tp)
		assert.IsType(t, &dst.InterfaceType{}, got)
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
