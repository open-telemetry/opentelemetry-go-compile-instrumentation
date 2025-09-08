// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"fmt"
	"go/token"

	"github.com/dave/dst"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

func findFuncDecls(root *dst.File, lambda func(*dst.FuncDecl) bool) ([]*dst.FuncDecl, error) {
	funcDecls, err := ListFuncDecls(root)
	if err != nil {
		return nil, err
	}
	// The function with receiver and the function without receiver may have
	// the same name, so they need to be classified into the same name
	found := make([]*dst.FuncDecl, 0)
	for _, funcDecl := range funcDecls {
		if lambda(funcDecl) {
			found = append(found, funcDecl)
		}
	}
	return found, nil
}

func FindFuncDecl(root *dst.File, funcName string) (*dst.FuncDecl, error) {
	decls, err := findFuncDecls(root, func(funcDecl *dst.FuncDecl) bool {
		return funcDecl.Name.Name == funcName && !HasReceiver(funcDecl)
	})
	if err != nil {
		return nil, err
	}
	if len(decls) == 0 {
		//nolint:nilnil // no function declaration found is not an error
		return nil, nil
	}
	return decls[0], nil
}

func FindFuncDeclWithRecv(root *dst.File, funcName string) ([]*dst.FuncDecl, error) {
	decls, err := findFuncDecls(root, func(funcDecl *dst.FuncDecl) bool {
		return funcDecl.Name.Name == funcName
	})
	if err != nil {
		return nil, err
	}
	util.Assert(len(decls) <= 2, "sanity check")
	return decls, nil
}

func ListFuncDecls(root *dst.File) ([]*dst.FuncDecl, error) {
	funcDecls := make([]*dst.FuncDecl, 0)
	for _, decl := range root.Decls {
		funcDecl, ok := decl.(*dst.FuncDecl)
		if !ok {
			continue
		}
		funcDecls = append(funcDecls, funcDecl)
	}
	return funcDecls, nil
}

func HasReceiver(fn *dst.FuncDecl) bool {
	return fn.Recv != nil && len(fn.Recv.List) > 0
}

func MakeUnusedIdent(ident *dst.Ident) *dst.Ident {
	ident.Name = IdentIgnore
	return ident
}

func IsUnusedIdent(ident *dst.Ident) bool {
	return ident.Name == IdentIgnore
}

func IsStringLit(expr dst.Expr, val string) bool {
	lit, ok := expr.(*dst.BasicLit)
	return ok &&
		lit.Kind == token.STRING &&
		lit.Value == fmt.Sprintf("%q", val)
}

func IsInterfaceType(typ dst.Expr) bool {
	_, ok := typ.(*dst.InterfaceType)
	return ok
}

func IsEllipsis(typ dst.Expr) bool {
	_, ok := typ.(*dst.Ellipsis)
	return ok
}
