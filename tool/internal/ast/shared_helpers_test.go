// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMakeAndIsUnusedIdent(t *testing.T) {
	id := Ident("foo")
	assert.False(t, IsUnusedIdent(id))

	got := MakeUnusedIdent(id)
	assert.Same(t, id, got)
	assert.Equal(t, IdentIgnore, got.Name)
	assert.True(t, IsUnusedIdent(got))
}

func TestIsStringLit(t *testing.T) {
	lit := StringLit("hello")
	assert.True(t, IsStringLit(lit, "hello"))
	assert.False(t, IsStringLit(lit, "world"))

	// A non-string-literal expression is never a string literal.
	assert.False(t, IsStringLit(Ident("hello"), "hello"))

	// An integer literal has the wrong Kind.
	assert.False(t, IsStringLit(IntLit(3), "3"))
}

func TestIsInterfaceType(t *testing.T) {
	assert.True(t, IsInterfaceType(InterfaceType()))
	// "any" is treated as interface{}.
	assert.True(t, IsInterfaceType(Ident("any")))
	// Other identifiers are not interface types.
	assert.False(t, IsInterfaceType(Ident("string")))
}

func TestIsEllipsis(t *testing.T) {
	assert.True(t, IsEllipsis(Ellipsis(Ident("int"))))
	assert.False(t, IsEllipsis(Ident("int")))
}

func TestAddStructField(t *testing.T) {
	const src = `package main

type S struct {
	A int
}
`
	p := NewAstParser()
	file, err := p.ParseSource(src)
	require.NoError(t, err)

	st := FindStructType(file, "S")
	require.NotNil(t, st)

	AddStructField(st, "B", "string")

	// The struct now has two fields, the new one named B of type string.
	require.Len(t, st.Fields.List, 2)
	newField := st.Fields.List[1]
	require.Len(t, newField.Names, 1)
	assert.Equal(t, "B", newField.Names[0].Name)
	assert.Equal(t, "string", newField.Type.(*dst.Ident).Name)
}

func TestStripGenericTypes(t *testing.T) {
	tests := []struct {
		name string
		expr dst.Expr
		want string
	}{
		{
			name: "value receiver",
			expr: Ident("MyStruct"),
			want: "MyStruct",
		},
		{
			name: "pointer receiver",
			expr: &dst.StarExpr{X: Ident("MyStruct")},
			want: "*MyStruct",
		},
		{
			name: "generic value receiver single param",
			expr: &dst.IndexExpr{X: Ident("GenStruct"), Index: Ident("T")},
			want: "GenStruct",
		},
		{
			name: "generic pointer receiver single param",
			expr: &dst.StarExpr{X: &dst.IndexExpr{X: Ident("GenStruct"), Index: Ident("T")}},
			want: "*GenStruct",
		},
		{
			name: "generic value receiver multiple params",
			expr: &dst.IndexListExpr{X: Ident("GenStruct"), Indices: []dst.Expr{Ident("T"), Ident("U")}},
			want: "GenStruct",
		},
		{
			name: "generic pointer receiver multiple params",
			expr: &dst.StarExpr{
				X: &dst.IndexListExpr{X: Ident("GenStruct"), Indices: []dst.Expr{Ident("T"), Ident("U")}},
			},
			want: "*GenStruct",
		},
		{
			name: "unrecognized expression yields empty",
			expr: &dst.SelectorExpr{X: Ident("pkg"), Sel: Ident("Type")},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, stripGenericTypes(tt.expr))
		})
	}
}
