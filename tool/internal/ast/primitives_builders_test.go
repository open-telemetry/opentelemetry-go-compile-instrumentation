// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"go/token"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIdent(t *testing.T) {
	id := Ident("foo")
	require.NotNil(t, id)
	assert.Equal(t, "foo", id.Name)
}

func TestNil(t *testing.T) {
	id := Nil()
	require.NotNil(t, id)
	assert.Equal(t, IdentNil, id.Name)
}

func TestAddressOf(t *testing.T) {
	expr := AddressOf("x")
	require.NotNil(t, expr)
	assert.Equal(t, token.AND, expr.Op)
	inner, ok := expr.X.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "x", inner.Name)
}

func TestStringLit(t *testing.T) {
	lit := StringLit("hello")
	require.NotNil(t, lit)
	assert.Equal(t, token.STRING, lit.Kind)
	// Value must be a quoted Go string literal.
	assert.Equal(t, `"hello"`, lit.Value)
}

func TestIntLit(t *testing.T) {
	lit := IntLit(42)
	require.NotNil(t, lit)
	assert.Equal(t, token.INT, lit.Kind)
	assert.Equal(t, "42", lit.Value)

	assert.Equal(t, "-7", IntLit(-7).Value)
}

func TestBlock(t *testing.T) {
	block := Block(EmptyStmt())
	require.NotNil(t, block)
	require.Len(t, block.List, 1)
	_, ok := block.List[0].(*dst.EmptyStmt)
	assert.True(t, ok)
}

func TestBlockStmts(t *testing.T) {
	block := BlockStmts(EmptyStmt(), EmptyStmt(), EmptyStmt())
	require.NotNil(t, block)
	assert.Len(t, block.List, 3)

	empty := BlockStmts()
	require.NotNil(t, empty)
	assert.Empty(t, empty.List)
}

func TestExprs(t *testing.T) {
	exprs := Exprs(Ident("a"), Ident("b"))
	require.Len(t, exprs, 2)
	assert.Empty(t, Exprs())
}

func TestStmts(t *testing.T) {
	stmts := Stmts(EmptyStmt(), EmptyStmt())
	require.Len(t, stmts, 2)
	assert.Empty(t, Stmts())
}

func TestSelectorExpr(t *testing.T) {
	x := Ident("pkg")
	sel := SelectorExpr(x, "Field")
	require.NotNil(t, sel)
	assert.Equal(t, "Field", sel.Sel.Name)
	base, ok := sel.X.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "pkg", base.Name)

	// The base expression is cloned, so mutating the source must not leak in.
	x.Name = "changed"
	assert.Equal(t, "pkg", sel.X.(*dst.Ident).Name)
}

func TestIndexExpr(t *testing.T) {
	idx := IndexExpr(Ident("m"), IntLit(0))
	require.NotNil(t, idx)
	base, ok := idx.X.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "m", base.Name)
	i, ok := idx.Index.(*dst.BasicLit)
	require.True(t, ok)
	assert.Equal(t, "0", i.Value)
}

func TestIndexListExpr(t *testing.T) {
	indices := []dst.Expr{Ident("T"), Ident("U")}
	idx := IndexListExpr(Ident("Generic"), indices)
	require.NotNil(t, idx)
	base, ok := idx.X.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "Generic", base.Name)
	assert.Len(t, idx.Indices, 2)
}

func TestTypeAssertExpr(t *testing.T) {
	ta := TypeAssertExpr(Ident("v"), Ident("string"))
	require.NotNil(t, ta)
	x, ok := ta.X.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "v", x.Name)
	typ, ok := ta.Type.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "string", typ.Name)
}

func TestParenExpr(t *testing.T) {
	p := ParenExpr(Ident("a"))
	require.NotNil(t, p)
	inner, ok := p.X.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "a", inner.Name)
}

func TestBoolLiterals(t *testing.T) {
	assert.Equal(t, IdentTrue, BoolTrue().Value)
	assert.Equal(t, IdentFalse, BoolFalse().Value)
}

func TestInterfaceType(t *testing.T) {
	it := InterfaceType()
	require.NotNil(t, it)
	require.NotNil(t, it.Methods)
	assert.True(t, it.Methods.Opening)
	assert.True(t, it.Methods.Closing)
}

func TestArrayType(t *testing.T) {
	at := ArrayType(Ident("byte"))
	require.NotNil(t, at)
	elt, ok := at.Elt.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "byte", elt.Name)
}

func TestEllipsis(t *testing.T) {
	e := Ellipsis(Ident("int"))
	require.NotNil(t, e)
	elt, ok := e.Elt.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "int", elt.Name)
}

func TestIfStmt(t *testing.T) {
	body := Block(EmptyStmt())
	elseBody := Block(EmptyStmt())
	stmt := IfStmt(AssignStmt(Ident("a"), Ident("b")), BoolTrue(), body, elseBody)
	require.NotNil(t, stmt)
	assert.NotNil(t, stmt.Init)
	assert.NotNil(t, stmt.Cond)
	require.NotNil(t, stmt.Body)
	assert.Len(t, stmt.Body.List, 1)
	assert.NotNil(t, stmt.Else)
}

func TestIfNotNilStmt(t *testing.T) {
	body := Block(EmptyStmt())

	t.Run("without else", func(t *testing.T) {
		stmt := IfNotNilStmt(Ident("err"), body, nil)
		require.NotNil(t, stmt)
		assert.Nil(t, stmt.Else)
		cond, ok := stmt.Cond.(*dst.BinaryExpr)
		require.True(t, ok)
		assert.Equal(t, token.NEQ, cond.Op)
		x, ok := cond.X.(*dst.Ident)
		require.True(t, ok)
		assert.Equal(t, "err", x.Name)
		y, ok := cond.Y.(*dst.Ident)
		require.True(t, ok)
		assert.Equal(t, IdentNil, y.Name)
	})

	t.Run("with else", func(t *testing.T) {
		stmt := IfNotNilStmt(Ident("err"), body, Block(EmptyStmt()))
		require.NotNil(t, stmt)
		assert.NotNil(t, stmt.Else)
	})
}

func TestEmptyStmt(t *testing.T) {
	assert.NotNil(t, EmptyStmt())
}

func TestExprStmt(t *testing.T) {
	stmt := ExprStmt(Ident("x"))
	require.NotNil(t, stmt)
	inner, ok := stmt.X.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "x", inner.Name)
}

func TestDeferStmt(t *testing.T) {
	call := CallTo("Close", nil, nil)
	stmt := DeferStmt(call)
	require.NotNil(t, stmt)
	require.NotNil(t, stmt.Call)
	fun, ok := stmt.Call.Fun.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "Close", fun.Name)
}

func TestReturnStmt(t *testing.T) {
	stmt := ReturnStmt(Exprs(Ident("a"), Ident("b")))
	require.NotNil(t, stmt)
	assert.Len(t, stmt.Results, 2)

	empty := ReturnStmt(nil)
	require.NotNil(t, empty)
	assert.Empty(t, empty.Results)
}

func TestAssignStmt(t *testing.T) {
	stmt := AssignStmt(Ident("a"), Ident("b"))
	require.NotNil(t, stmt)
	assert.Equal(t, token.ASSIGN, stmt.Tok)
	require.Len(t, stmt.Lhs, 1)
	require.Len(t, stmt.Rhs, 1)
	assert.Equal(t, "a", stmt.Lhs[0].(*dst.Ident).Name)
	assert.Equal(t, "b", stmt.Rhs[0].(*dst.Ident).Name)
}

func TestDefineStmts(t *testing.T) {
	stmt := DefineStmts(Exprs(Ident("a"), Ident("b")), Exprs(Ident("x")))
	require.NotNil(t, stmt)
	assert.Equal(t, token.DEFINE, stmt.Tok)
	assert.Len(t, stmt.Lhs, 2)
	assert.Len(t, stmt.Rhs, 1)
}

func TestSwitchCase(t *testing.T) {
	cc := SwitchCase(Exprs(Ident("A")), Stmts(EmptyStmt()))
	require.NotNil(t, cc)
	assert.Len(t, cc.List, 1)
	assert.Len(t, cc.Body, 1)
}

func TestDereferenceOf(t *testing.T) {
	star := DereferenceOf(Ident("p"))
	require.NotNil(t, star)
	x, ok := star.X.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "p", x.Name)
}

func TestField(t *testing.T) {
	f := Field("name", Ident("string"))
	require.NotNil(t, f)
	require.Len(t, f.Names, 1)
	assert.Equal(t, "name", f.Names[0].Name)
	typ, ok := f.Type.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "string", typ.Name)
}

func TestImportDecl(t *testing.T) {
	decl := ImportDecl("alias", "example.com/pkg")
	require.NotNil(t, decl)
	assert.Equal(t, token.IMPORT, decl.Tok)
	require.Len(t, decl.Specs, 1)
	spec, ok := decl.Specs[0].(*dst.ImportSpec)
	require.True(t, ok)
	assert.Equal(t, "alias", spec.Name.Name)
	assert.Equal(t, `"example.com/pkg"`, spec.Path.Value)
}

func TestVarDecl(t *testing.T) {
	decl := VarDecl("x", IntLit(1))
	require.NotNil(t, decl)
	assert.Equal(t, token.VAR, decl.Tok)
	require.Len(t, decl.Specs, 1)
	spec, ok := decl.Specs[0].(*dst.ValueSpec)
	require.True(t, ok)
	require.Len(t, spec.Names, 1)
	assert.Equal(t, "x", spec.Names[0].Name)
	require.Len(t, spec.Values, 1)
}

func TestLineComments(t *testing.T) {
	decs := LineComments("// first", "// second")
	assert.Equal(t, dst.NewLine, decs.Before)
	assert.Equal(t, dst.Decorations{"// first", "// second"}, decs.Start)
}

func TestKeyValueExpr(t *testing.T) {
	kv := KeyValueExpr("Key", Ident("value"))
	require.NotNil(t, kv)
	key, ok := kv.Key.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "Key", key.Name)
	val, ok := kv.Value.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "value", val.Name)
}

func TestCompositeLit(t *testing.T) {
	cl := CompositeLit(Ident("T"), Exprs(Ident("a"), Ident("b")))
	require.NotNil(t, cl)
	typ, ok := cl.Type.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "T", typ.Name)
	assert.Len(t, cl.Elts, 2)
}

func TestStructLit(t *testing.T) {
	lit := StructLit("MyStruct", KeyValueExpr("A", IntLit(1)), KeyValueExpr("B", IntLit(2)))
	require.NotNil(t, lit)
	// StructLit returns a pointer literal: &MyStruct{...}
	unary, ok := lit.(*dst.UnaryExpr)
	require.True(t, ok)
	assert.Equal(t, token.AND, unary.Op)
	cl, ok := unary.X.(*dst.CompositeLit)
	require.True(t, ok)
	typ, ok := cl.Type.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "MyStruct", typ.Name)
	assert.Len(t, cl.Elts, 2)
}

func TestStructLitNoFields(t *testing.T) {
	lit := StructLit("Empty")
	unary, ok := lit.(*dst.UnaryExpr)
	require.True(t, ok)
	cl, ok := unary.X.(*dst.CompositeLit)
	require.True(t, ok)
	assert.Empty(t, cl.Elts)
}
