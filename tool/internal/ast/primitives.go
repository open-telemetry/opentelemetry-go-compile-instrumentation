// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"fmt"
	"go/token"
	"strconv"

	"github.com/dave/dst"
)

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

func AddressOf(expr dst.Expr) *dst.UnaryExpr {
	return &dst.UnaryExpr{Op: token.AND, X: dst.Clone(expr).(dst.Expr)}
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
		Value: fmt.Sprintf("%q", value),
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
	return &dst.SelectorExpr{
		X:   dst.Clone(x).(dst.Expr),
		Sel: Ident(sel),
	}
}

func IndexExpr(x dst.Expr, index dst.Expr) *dst.IndexExpr {
	return &dst.IndexExpr{
		X:     dst.Clone(x).(dst.Expr),
		Index: dst.Clone(index).(dst.Expr),
	}
}

func TypeAssertExpr(x dst.Expr, typ dst.Expr) *dst.TypeAssertExpr {
	return &dst.TypeAssertExpr{
		X:    x,
		Type: dst.Clone(typ).(dst.Expr),
	}
}

func ParenExpr(x dst.Expr) *dst.ParenExpr {
	return &dst.ParenExpr{
		X: dst.Clone(x).(dst.Expr),
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
	return &dst.IfStmt{
		Init: dst.Clone(init).(dst.Stmt),
		Cond: dst.Clone(cond).(dst.Expr),
		Body: dst.Clone(body).(*dst.BlockStmt),
		Else: dst.Clone(elseBody).(*dst.BlockStmt),
	}
}

func IfNotNilStmt(cond dst.Expr, body, elseBody *dst.BlockStmt) *dst.IfStmt {
	var elseB dst.Stmt
	if elseBody == nil {
		elseB = nil
	} else {
		elseB = dst.Clone(elseBody).(dst.Stmt)
	}
	return &dst.IfStmt{
		Cond: &dst.BinaryExpr{
			X:  dst.Clone(cond).(dst.Expr),
			Op: token.NEQ,
			Y:  &dst.Ident{Name: IdentNil},
		},
		Body: dst.Clone(body).(*dst.BlockStmt),
		Else: elseB,
	}
}

func EmptyStmt() *dst.EmptyStmt {
	return &dst.EmptyStmt{}
}

func ExprStmt(expr dst.Expr) *dst.ExprStmt {
	return &dst.ExprStmt{X: dst.Clone(expr).(dst.Expr)}
}

func DeferStmt(call *dst.CallExpr) *dst.DeferStmt {
	return &dst.DeferStmt{Call: dst.Clone(call).(*dst.CallExpr)}
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

func Field(name string, typ dst.Expr) *dst.Field {
	newField := &dst.Field{
		Names: []*dst.Ident{Ident(name)},
		Type:  typ,
	}
	return newField
}

func ImportDecl(alias, path string) *dst.GenDecl {
	return &dst.GenDecl{
		Tok: token.IMPORT,
		Specs: []dst.Spec{
			&dst.ImportSpec{
				Name: dst.NewIdent(alias),
				Path: &dst.BasicLit{Value: fmt.Sprintf("%q", path)},
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
