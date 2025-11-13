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

func CallTo(name string, args []dst.Expr) *dst.CallExpr {
	return &dst.CallExpr{
		Fun:  &dst.Ident{Name: name},
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
	e, ok := dst.Clone(x).(dst.Expr)
	util.Assert(ok, "x is not a Expr")
	return &dst.SelectorExpr{
		X:   e,
		Sel: Ident(sel),
	}
}

func IndexExpr(x, index dst.Expr) *dst.IndexExpr {
	e, ok := dst.Clone(x).(dst.Expr)
	util.Assert(ok, "x is not a Expr")
	i, ok := dst.Clone(index).(dst.Expr)
	util.Assert(ok, "index is not a Expr")
	return &dst.IndexExpr{
		X:     e,
		Index: i,
	}
}

func TypeAssertExpr(x, t dst.Expr) *dst.TypeAssertExpr {
	e, ok := dst.Clone(t).(dst.Expr)
	util.Assert(ok, "t is not a Expr")
	return &dst.TypeAssertExpr{
		X:    x,
		Type: e,
	}
}

func ParenExpr(x dst.Expr) *dst.ParenExpr {
	e, ok := dst.Clone(x).(dst.Expr)
	util.Assert(ok, "x is not a Expr")
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
	return &dst.InterfaceType{Methods: &dst.FieldList{List: nil}}
}

func ArrayType(elem dst.Expr) *dst.ArrayType {
	return &dst.ArrayType{Elt: elem}
}

func IfStmt(init dst.Stmt, cond dst.Expr, body, elseBody *dst.BlockStmt) *dst.IfStmt {
	i, ok := dst.Clone(init).(dst.Stmt)
	util.Assert(ok, "init is not a Stmt")
	e, ok := dst.Clone(cond).(dst.Expr)
	util.Assert(ok, "cond is not a Expr")
	b, ok := dst.Clone(body).(*dst.BlockStmt)
	util.Assert(ok, "body is not a BlockStmt")
	eb, ok := dst.Clone(elseBody).(*dst.BlockStmt)
	util.Assert(ok, "elseBody is not a BlockStmt")
	return &dst.IfStmt{Init: i, Cond: e, Body: b, Else: eb}
}

func IfNotNilStmt(cond dst.Expr, body, elseBody *dst.BlockStmt) *dst.IfStmt {
	var elseB dst.Stmt
	if elseBody == nil {
		elseB = nil
	} else {
		e, ok := dst.Clone(elseBody).(dst.Stmt)
		util.Assert(ok, "elseBody is not a Stmt")
		elseB = e
	}
	e, ok := dst.Clone(cond).(dst.Expr)
	util.Assert(ok, "cond is not a Expr")
	b, ok := dst.Clone(body).(*dst.BlockStmt)
	util.Assert(ok, "body is not a BlockStmt")
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
	e, ok := dst.Clone(expr).(dst.Expr)
	util.Assert(ok, "expr is not a Expr")
	return &dst.ExprStmt{X: e}
}

func DeferStmt(call *dst.CallExpr) *dst.DeferStmt {
	c, ok := dst.Clone(call).(*dst.CallExpr)
	util.Assert(ok, "call is not a CallExpr")
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
