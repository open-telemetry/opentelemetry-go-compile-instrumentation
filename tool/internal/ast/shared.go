// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"fmt"
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

// stripGenericTypes extracts the base type name from a receiver expression,
// handling both generic and non-generic types.
// For example:
// - *MyStruct -> *MyStruct
// - MyStruct -> MyStruct
// - *GenStruct[T] -> *GenStruct
// - GenStruct[T] -> GenStruct
func stripGenericTypes(recvTypeExpr dst.Expr) string {
	switch expr := recvTypeExpr.(type) {
	case *dst.StarExpr: // func (*Recv)T or func (*Recv[T])T
		// Check if X is an Ident (non-generic) or IndexExpr/IndexListExpr (generic)
		switch x := expr.X.(type) {
		case *dst.Ident:
			// Non-generic pointer receiver: *MyStruct
			return "*" + x.Name
		case *dst.IndexExpr:
			// Generic pointer receiver with single type param: *GenStruct[T]
			if baseIdent, ok := x.X.(*dst.Ident); ok {
				return "*" + baseIdent.Name
			}
		case *dst.IndexListExpr:
			// Generic pointer receiver with multiple type params: *GenStruct[T, U]
			if baseIdent, ok := x.X.(*dst.Ident); ok {
				return "*" + baseIdent.Name
			}
		}
	case *dst.Ident: // func (Recv)T
		return expr.Name
	case *dst.IndexExpr:
		// Generic value receiver with single type param: GenStruct[T]
		if baseIdent, ok := expr.X.(*dst.Ident); ok {
			return baseIdent.Name
		}
	case *dst.IndexListExpr:
		// Generic value receiver with multiple type params: GenStruct[T, U]
		if baseIdent, ok := expr.X.(*dst.Ident); ok {
			return baseIdent.Name
		}
	}
	return ""
}

func FindFuncDecl(root *dst.File, funcName, recv string) *dst.FuncDecl {
	decls := findFuncDecls(root, func(funcDecl *dst.FuncDecl) bool {
		// Receiver type is ignored, match func name only
		name := funcDecl.Name.Name
		if recv == "" {
			return name == funcName && !HasReceiver(funcDecl)
		}
		// Receiver type is specified, but target function does not have receiver
		// That's not what we want
		if !HasReceiver(funcDecl) {
			return false
		}

		// Receiver type is specified, and target function has receiver
		// Match both func name and receiver type
		recvTypeExpr := funcDecl.Recv.List[0].Type
		baseType := stripGenericTypes(recvTypeExpr)

		if baseType == "" {
			msg := fmt.Sprintf("unexpected receiver type: %T", recvTypeExpr)
			util.Unimplemented(msg)
		}

		return baseType == recv && name == funcName
	})

	if len(decls) == 0 {
		return nil
	}
	return decls[0]
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

func AddStructField(decl dst.Decl, name, t string) {
	gen := util.AssertType[*dst.GenDecl](decl)
	fd := Field(name, Ident(t))
	ty := util.AssertType[*dst.TypeSpec](gen.Specs[0])
	st := util.AssertType[*dst.StructType](ty.Type)
	st.Fields.List = append(st.Fields.List, fd)
}

// SplitMultiNameFields splits fields that have multiple names into separate fields.
// For example, a field like "a, b int" becomes two fields: "a int" and "b int".
func SplitMultiNameFields(fieldList *dst.FieldList) *dst.FieldList {
	if fieldList == nil {
		return nil
	}
	result := &dst.FieldList{List: []*dst.Field{}}
	for _, field := range fieldList.List {
		// Handle unnamed fields (e.g., embedded types) or fields with single/multiple names
		namesToProcess := field.Names
		if len(namesToProcess) == 0 {
			// For unnamed fields, create one field with no names
			namesToProcess = []*dst.Ident{nil}
		}

		for _, name := range namesToProcess {
			clonedType := util.AssertType[dst.Expr](dst.Clone(field.Type))

			var names []*dst.Ident
			if name != nil {
				clonedName := util.AssertType[*dst.Ident](dst.Clone(name))
				names = []*dst.Ident{clonedName}
			}

			newField := &dst.Field{
				Names: names,
				Type:  clonedType,
			}
			result.List = append(result.List, newField)
		}
	}
	return result
}

// CloneTypeParams safely clones a type parameter field list for generic functions.
// Returns nil if the input is nil.
func CloneTypeParams(typeParams *dst.FieldList) *dst.FieldList {
	if typeParams == nil {
		return nil
	}
	return util.AssertType[*dst.FieldList](dst.Clone(typeParams))
}
