// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"go/token"
	"strconv"

	"github.com/dave/dst"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

// -----------------------------------------------------------------------------
// AST Primitives
//
// This file provides essential primitives for AST manipulation, including common
// identifier constants, type checking, expression and so on.
//
// The primitives defined here serve as building blocks for higher-level AST
// operations throughout the instrumentation toolchain, ensuring consistent
// handling of common AST patterns and reducing code duplication.

const (
	IdentNil    = "nil"
	IdentTrue   = "true"
	IdentFalse  = "false"
	IdentIgnore = "_"
)

func Ident(name string) *dst.Ident {
	return &dst.Ident{
		Name: name,
	}
}

func Nil() *dst.Ident {
	return &dst.Ident{Name: IdentNil}
}

func AddressOf(name string) *dst.UnaryExpr {
	return &dst.UnaryExpr{Op: token.AND, X: Ident(name)}
}

// CallTo creates a call expression to a function with optional type arguments for generics.
// For non-generic functions (typeArgs is nil or empty), creates a simple call: Foo(args...)
// For generic functions with type arguments, creates: Foo[T1, T2](args...)
func CallTo(name string, typeArgs *dst.FieldList, args []dst.Expr) *dst.CallExpr {
	if typeArgs == nil || len(typeArgs.List) == 0 {
		return &dst.CallExpr{
			Fun:  &dst.Ident{Name: name},
			Args: args,
		}
	}

	var indices []dst.Expr
	for _, field := range typeArgs.List {
		for _, ident := range field.Names {
			indices = append(indices, Ident(ident.Name))
		}
	}
	var fun dst.Expr
	if len(indices) == 1 {
		fun = IndexExpr(Ident(name), indices[0])
	} else {
		fun = IndexListExpr(Ident(name), indices)
	}
	return &dst.CallExpr{
		Fun:  fun,
		Args: args,
	}
}

func StringLit(value string) *dst.BasicLit {
	return &dst.BasicLit{
		Kind:  token.STRING,
		Value: strconv.Quote(value),
	}
}

func IntLit(value int) *dst.BasicLit {
	return &dst.BasicLit{
		Kind:  token.INT,
		Value: strconv.Itoa(value),
	}
}

func Block(stmt dst.Stmt) *dst.BlockStmt {
	return &dst.BlockStmt{
		List: []dst.Stmt{
			stmt,
		},
	}
}

func BlockStmts(stmts ...dst.Stmt) *dst.BlockStmt {
	return &dst.BlockStmt{
		List: stmts,
	}
}

func Exprs(exprs ...dst.Expr) []dst.Expr {
	return exprs
}

func Stmts(stmts ...dst.Stmt) []dst.Stmt {
	return stmts
}

func SelectorExpr(x dst.Expr, sel string) *dst.SelectorExpr {
	e := util.AssertType[dst.Expr](dst.Clone(x))
	return &dst.SelectorExpr{
		X:   e,
		Sel: Ident(sel),
	}
}

func IndexExpr(x, index dst.Expr) *dst.IndexExpr {
	e := util.AssertType[dst.Expr](dst.Clone(x))
	i := util.AssertType[dst.Expr](dst.Clone(index))
	return &dst.IndexExpr{
		X:     e,
		Index: i,
	}
}

func IndexListExpr(x dst.Expr, indices []dst.Expr) *dst.IndexListExpr {
	e := util.AssertType[dst.Expr](dst.Clone(x))
	return &dst.IndexListExpr{
		X:       e,
		Indices: indices,
	}
}

func TypeAssertExpr(x, t dst.Expr) *dst.TypeAssertExpr {
	e := util.AssertType[dst.Expr](dst.Clone(t))
	return &dst.TypeAssertExpr{
		X:    x,
		Type: e,
	}
}

func ParenExpr(x dst.Expr) *dst.ParenExpr {
	e := util.AssertType[dst.Expr](dst.Clone(x))
	return &dst.ParenExpr{
		X: e,
	}
}

func BoolTrue() *dst.BasicLit {
	return &dst.BasicLit{Value: IdentTrue}
}

func BoolFalse() *dst.BasicLit {
	return &dst.BasicLit{Value: IdentFalse}
}

func InterfaceType() *dst.InterfaceType {
	return &dst.InterfaceType{
		Methods: &dst.FieldList{Opening: true, Closing: true},
	}
}

func ArrayType(elem dst.Expr) *dst.ArrayType {
	return &dst.ArrayType{Elt: elem}
}

func Ellipsis(elem dst.Expr) *dst.Ellipsis {
	return &dst.Ellipsis{Elt: elem}
}

func IfStmt(init dst.Stmt, cond dst.Expr, body, elseBody *dst.BlockStmt) *dst.IfStmt {
	i := util.AssertType[dst.Stmt](dst.Clone(init))
	e := util.AssertType[dst.Expr](dst.Clone(cond))
	b := util.AssertType[*dst.BlockStmt](dst.Clone(body))
	eb := util.AssertType[*dst.BlockStmt](dst.Clone(elseBody))
	return &dst.IfStmt{Init: i, Cond: e, Body: b, Else: eb}
}

func IfNotNilStmt(cond dst.Expr, body, elseBody *dst.BlockStmt) *dst.IfStmt {
	var elseB dst.Stmt
	if elseBody == nil {
		elseB = nil
	} else {
		e := util.AssertType[dst.Stmt](dst.Clone(elseBody))
		elseB = e
	}
	e := util.AssertType[dst.Expr](dst.Clone(cond))
	b := util.AssertType[*dst.BlockStmt](dst.Clone(body))
	return &dst.IfStmt{
		Cond: &dst.BinaryExpr{
			X:  e,
			Op: token.NEQ,
			Y:  &dst.Ident{Name: IdentNil},
		},
		Body: b,
		Else: elseB,
	}
}

func EmptyStmt() *dst.EmptyStmt {
	return &dst.EmptyStmt{}
}

func ExprStmt(expr dst.Expr) *dst.ExprStmt {
	e := util.AssertType[dst.Expr](dst.Clone(expr))
	return &dst.ExprStmt{X: e}
}

func DeferStmt(call *dst.CallExpr) *dst.DeferStmt {
	c := util.AssertType[*dst.CallExpr](dst.Clone(call))
	return &dst.DeferStmt{Call: c}
}

func ReturnStmt(results []dst.Expr) *dst.ReturnStmt {
	return &dst.ReturnStmt{Results: results}
}

func AssignStmt(lhs, rhs dst.Expr) *dst.AssignStmt {
	return &dst.AssignStmt{
		Lhs: []dst.Expr{lhs},
		Tok: token.ASSIGN,
		Rhs: []dst.Expr{rhs},
	}
}

func DefineStmts(lhs, rhs []dst.Expr) *dst.AssignStmt {
	return &dst.AssignStmt{
		Lhs: lhs,
		Tok: token.DEFINE,
		Rhs: rhs,
	}
}

func SwitchCase(list []dst.Expr, stmts []dst.Stmt) *dst.CaseClause {
	return &dst.CaseClause{
		List: list,
		Body: stmts,
	}
}

func DereferenceOf(expr dst.Expr) *dst.StarExpr {
	return &dst.StarExpr{X: expr}
}

func Field(name string, t dst.Expr) *dst.Field {
	newField := &dst.Field{
		Names: []*dst.Ident{Ident(name)},
		Type:  t,
	}
	return newField
}

func ImportDecl(alias, path string) *dst.GenDecl {
	return &dst.GenDecl{
		Tok: token.IMPORT,
		Specs: []dst.Spec{
			&dst.ImportSpec{
				Name: dst.NewIdent(alias),
				Path: &dst.BasicLit{Value: strconv.Quote(path)},
			},
		},
	}
}

func VarDecl(name string, value dst.Expr) *dst.GenDecl {
	return &dst.GenDecl{
		Tok: token.VAR,
		Specs: []dst.Spec{
			&dst.ValueSpec{
				Names: []*dst.Ident{
					{Name: name},
				},
				Values: []dst.Expr{
					value,
				},
			},
		},
	}
}

func LineComments(comments ...string) dst.NodeDecs {
	return dst.NodeDecs{
		Before: dst.NewLine,
		Start:  dst.Decorations(comments),
	}
}

func KeyValueExpr(key string, value dst.Expr) *dst.KeyValueExpr {
	return &dst.KeyValueExpr{
		Key:   Ident(key),
		Value: value,
	}
}

func CompositeLit(t dst.Expr, elts []dst.Expr) *dst.CompositeLit {
	return &dst.CompositeLit{
		Type: t,
		Elts: elts,
	}
}

func StructLit(typeName string, fields ...*dst.KeyValueExpr) dst.Expr {
	exprs := make([]dst.Expr, len(fields))
	for i, field := range fields {
		exprs[i] = field
	}
	return &dst.UnaryExpr{
		Op: token.AND,
		X:  CompositeLit(Ident(typeName), exprs),
	}
}
