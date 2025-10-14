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
// AST Shared Utilities
//
// This file contains shared utility functions for AST traversal and manipulation.
// It provides common operations for finding, filtering, and processing AST nodes

func findFuncDecls(root *dst.File, lambda func(*dst.FuncDecl) bool) []*dst.FuncDecl {
	funcDecls := ListFuncDecls(root)

	// The function with receiver and the function without receiver may have
	// the same name, so they need to be classified into the same name
	found := make([]*dst.FuncDecl, 0)
	for _, funcDecl := range funcDecls {
		if lambda(funcDecl) {
			found = append(found, funcDecl)
		}
	}
	return found
}

func FindFuncDeclWithoutRecv(root *dst.File, funcName string) *dst.FuncDecl {
	decls := findFuncDecls(root, func(funcDecl *dst.FuncDecl) bool {
		return funcDecl.Name.Name == funcName && !HasReceiver(funcDecl)
	})

	if len(decls) == 0 {
		return nil
	}
	return decls[0]
}

func FindFuncDecl(root *dst.File, funcName string) []*dst.FuncDecl {
	const maxMatchDecls = 2
	decls := findFuncDecls(root, func(funcDecl *dst.FuncDecl) bool {
		return funcDecl.Name.Name == funcName
	})

	// one with receiver and one without receiver, at most two
	util.Assert(len(decls) <= maxMatchDecls, "sanity check")
	return decls
}

func ListFuncDecls(root *dst.File) []*dst.FuncDecl {
	funcDecls := make([]*dst.FuncDecl, 0)
	for _, decl := range root.Decls {
		funcDecl, ok := decl.(*dst.FuncDecl)
		if !ok {
			continue
		}
		funcDecls = append(funcDecls, funcDecl)
	}
	return funcDecls
}

func FindStructDecl(root *dst.File, structName string) *dst.GenDecl {
	for _, decl := range root.Decls {
		if genDecl, ok := decl.(*dst.GenDecl); ok && genDecl.Tok == token.TYPE {
			if typeSpec, ok1 := genDecl.Specs[0].(*dst.TypeSpec); ok1 {
				if typeSpec.Name.Name == structName {
					return genDecl
				}
			}
		}
	}
	return nil
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
	if !ok {
		return false
	}
	str, err := strconv.Unquote(lit.Value)
	if err != nil {
		return false
	}
	return lit.Kind == token.STRING && str == val
}

func IsInterfaceType(t dst.Expr) bool {
	_, ok := t.(*dst.InterfaceType)
	return ok
}

func IsEllipsis(t dst.Expr) bool {
	_, ok := t.(*dst.Ellipsis)
	return ok
}

func AddStructField(decl dst.Decl, name string, t string) {
	gen, ok := decl.(*dst.GenDecl)
	util.Assert(ok, "decl is not a GenDecl")
	fd := Field(name, Ident(t))
	ty, ok := gen.Specs[0].(*dst.TypeSpec)
	util.Assert(ok, "ty is not a TypeSpec")
	st, ok := ty.Type.(*dst.StructType)
	util.Assert(ok, "st is not a StructType")
	st.Fields.List = append(st.Fields.List, fd)
}
