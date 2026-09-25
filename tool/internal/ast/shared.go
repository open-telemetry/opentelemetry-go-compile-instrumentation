// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"fmt"
	"go/format"
	"go/token"
	"strconv"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

func findFuncDecls(root *dst.File, lambda func(*dst.FuncDecl) bool) []*dst.FuncDecl {
	funcDecls := listFuncDecls(root)

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

func findFuncDecl(root *dst.File, funcName, recv string) *dst.FuncDecl {
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

// FindFuncDecl finds the function declaration targeted by r, including
// name, receiver, and optional signature-filter matching.
//
// The returned bool reports whether a matching declaration was found. It is
// false both when no declaration matches r's function name and receiver, and
// when a declaration is found but does not satisfy r's signature filters. When
// the bool is false, the returned function declaration is nil.
func FindFuncDecl[R rule.InstFuncRule | rule.InstRawRule | rule.FilterDef](
	root *dst.File,
	r *R,
) (*dst.FuncDecl, bool, error) {
	var (
		funcName       string
		recv           string
		matchSignature bool
	)
	switch rr := any(r).(type) {
	case *rule.InstFuncRule:
		funcName = rr.Func
		recv = rr.Recv
		matchSignature = true
	case *rule.InstRawRule:
		funcName = rr.Func
		recv = rr.Recv
	case *rule.FilterDef:
		funcName = rr.HasFunc
		recv = rr.HasRecv
	}

	funcDecl := findFuncDecl(root, funcName, recv)
	if funcDecl == nil {
		return nil, false, nil
	}

	if !matchSignature {
		return funcDecl, true, nil
	}

	rr, ok := any(r).(*rule.InstFuncRule)
	if !ok {
		return nil, false, ex.Newf("unexpected %T value", r)
	}
	ok, err := funcDeclMatchesFilters(funcDecl, rr, root)
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return nil, false, nil
	}
	return funcDecl, true, nil
}

func listFuncDecls(root *dst.File) []*dst.FuncDecl {
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

// findVarDecl finds a package-level variable declaration by name.
// Returns the enclosing GenDecl and the matching ValueSpec, or nil if not found.
func findVarDecl(root *dst.File, name string) (*dst.GenDecl, *dst.ValueSpec) {
	return findValueDecl(root, name, token.VAR)
}

// findConstDecl finds a package-level constant declaration by name.
// Returns the enclosing GenDecl and the matching ValueSpec, or nil if not found.
func findConstDecl(root *dst.File, name string) (*dst.GenDecl, *dst.ValueSpec) {
	return findValueDecl(root, name, token.CONST)
}

func findValueDecl(root *dst.File, name string, tok token.Token) (*dst.GenDecl, *dst.ValueSpec) {
	for _, decl := range root.Decls {
		genDecl, ok := decl.(*dst.GenDecl)
		if !ok || genDecl.Tok != tok {
			continue
		}
		for _, spec := range genDecl.Specs {
			valueSpec, ok1 := spec.(*dst.ValueSpec)
			if !ok1 {
				continue
			}
			for _, ident := range valueSpec.Names {
				if ident.Name == name {
					return genDecl, valueSpec
				}
			}
		}
	}
	return nil, nil
}

// findTypeDecl finds a package-level type declaration by name (any kind: struct, interface, alias, etc).
func findTypeDecl(root *dst.File, name string) *dst.GenDecl {
	for _, decl := range root.Decls {
		genDecl, ok := decl.(*dst.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}
		for _, spec := range genDecl.Specs {
			typeSpec, ok1 := spec.(*dst.TypeSpec)
			if ok1 && typeSpec.Name.Name == name {
				return genDecl
			}
		}
	}
	return nil
}

// FindNamedDecl finds a package-level declaration by name and optional kind.
// kind may be "func", "var", "const", "type", or "" to match any.
// Returns the matched AST node (FuncDecl, ValueSpec, or GenDecl) or nil.
func FindNamedDecl(root *dst.File, name, kind string) dst.Node {
	switch kind {
	case "func":
		if n := FindFuncDeclWithoutRecv(root, name); n != nil {
			return n
		}
	case "var":
		if _, spec := findVarDecl(root, name); spec != nil {
			return spec
		}
	case "const":
		if _, spec := findConstDecl(root, name); spec != nil {
			return spec
		}
	case "type":
		if n := findTypeDecl(root, name); n != nil {
			return n
		}
	default:
		// Try all kinds, return first match
		if fn := FindFuncDeclWithoutRecv(root, name); fn != nil {
			return fn
		}
		if _, spec := findVarDecl(root, name); spec != nil {
			return spec
		}
		if _, spec := findConstDecl(root, name); spec != nil {
			return spec
		}
		if n := findTypeDecl(root, name); n != nil {
			return n
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

func isStringLit(expr dst.Expr, val string) bool {
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
	if _, ok := t.(*dst.InterfaceType); ok {
		return true
	}
	// "any" is the modern alias for interface{} (Go 1.18+), handle both
	ident, ok := t.(*dst.Ident)
	return ok && ident.Name == "any"
}

func IsEllipsis(t dst.Expr) bool {
	_, ok := t.(*dst.Ellipsis)
	return ok
}

// FindStructType returns the *dst.StructType declared under name, or nil if there
// is no such top-level type or the named type is not a struct (e.g. an interface,
// alias, or named non-struct type). Unlike findTypeDecl it resolves the specific
// spec by name, so it is correct for grouped `type ( ... )` blocks.
func FindStructType(root *dst.File, name string) *dst.StructType {
	gen := findTypeDecl(root, name)
	if gen == nil {
		return nil
	}
	for _, spec := range gen.Specs {
		ts, ok := spec.(*dst.TypeSpec)
		if !ok || ts.Name.Name != name {
			continue
		}
		st, ok := ts.Type.(*dst.StructType)
		if !ok {
			return nil
		}
		return st
	}
	return nil
}

// AddStructField appends a field named name of type t to the given struct.
func AddStructField(st *dst.StructType, name, t string) {
	st.Fields.List = append(st.Fields.List, Field(name, Ident(t)))
}

// funcDeclMatchesFilters reports whether funcDecl satisfies all signature
// sub-filters in r.  Returns true when no sub-filters are set.
//
// All non-empty filters are evaluated and must match (AND semantics).  Any
// combination of sub-filters is valid; they are checked in declaration order
// and evaluation stops at the first failure.
//
// Matching uses structural comparison of dst.Expr nodes (no type checker).
// For the scalar-type filters this means an exact type-name match rather than
// full interface-satisfaction checking.
//
// Qualified type names are resolved against imports, which maps the local
// identifier used at a use site to its real import path (see ImportAliasMap).
// Matching is therefore relative to the enclosing file's import declarations.
func funcDeclMatchesFilters(funcDecl *dst.FuncDecl, r *rule.InstFuncRule, root *dst.File) (bool, error) {
	if r.Signature == nil && r.SignatureContains == nil && r.Result == "" && r.LastResult == "" && r.Param == "" {
		return true, nil
	}
	imports := ImportAliasMap(root)
	ft := funcDecl.Type

	if r.Signature != nil {
		ok, err := matchesExactSignature(ft, r.Signature, imports)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	if r.SignatureContains != nil {
		ok, err := matchesSignatureContains(ft, r.SignatureContains, imports)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	if r.Result != "" {
		ok, err := fieldListContainsType(ft.Results, r.Result, imports)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	if r.LastResult != "" {
		ok, err := matchesLastResult(ft.Results, r.LastResult, imports)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	if r.Param != "" {
		ok, err := fieldListContainsType(ft.Params, r.Param, imports)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

// matchesExactSignature returns true when funcType has exactly the parameter
// and result types listed in sig, compared field-by-field in order.
func matchesExactSignature(ft *dst.FuncType, sig *rule.FuncSignature, imports map[string]string) (bool, error) {
	ok, err := matchesFieldList(sig.Args, ft.Params, imports)
	if err != nil || !ok {
		return ok, err
	}
	return matchesFieldList(sig.Returns, ft.Results, imports)
}

// matchesFieldList returns true when expected type strings match the types in
// fields exactly (same count, same order).
// Multi-name fields (e.g. "a, b int") are expanded inline so each name maps
// to exactly one type slot — without cloning AST nodes.
func matchesFieldList(expected []string, fields *dst.FieldList, imports map[string]string) (bool, error) {
	if len(expected) == 0 {
		return fields == nil || len(fields.List) == 0, nil
	}
	var types []dst.Expr
	if fields != nil {
		for _, f := range fields.List {
			if len(f.Names) == 0 {
				types = append(types, f.Type)
			} else {
				for range f.Names {
					types = append(types, f.Type)
				}
			}
		}
	}
	if len(expected) != len(types) {
		return false, nil
	}
	for i, typeStr := range expected {
		tn, err := parseTypeName(typeStr)
		if err != nil {
			return false, err
		}
		if !tn.matches(types[i], imports) {
			return false, nil
		}
	}
	return true, nil
}

// matchesSignatureContains returns true when funcType contains any of the
// expected argument types among its parameters OR any of the expected return
// types among its results.
func matchesSignatureContains(ft *dst.FuncType, sig *rule.FuncSignature, imports map[string]string) (bool, error) {
	for _, expected := range sig.Args {
		ok, err := fieldListContainsType(ft.Params, expected, imports)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	for _, expected := range sig.Returns {
		ok, err := fieldListContainsType(ft.Results, expected, imports)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

// matchesLastResult returns true when the last entry in fields matches typeStr.
func matchesLastResult(fields *dst.FieldList, typeStr string, imports map[string]string) (bool, error) {
	if fields == nil || len(fields.List) == 0 {
		return false, nil
	}
	tn, err := parseTypeName(typeStr)
	if err != nil {
		return false, err
	}
	return tn.matches(fields.List[len(fields.List)-1].Type, imports), nil
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

// RenderExpr renders expr back into Go source text.
func RenderExpr(expr dst.Expr) (string, error) {
	cloned := util.AssertType[dst.Expr](dst.Clone(expr))
	synthetic := &dst.File{
		Name: Ident("_"),
		Decls: []dst.Decl{
			&dst.FuncDecl{
				Name: Ident("_"),
				Type: &dst.FuncType{Params: &dst.FieldList{}},
				Body: &dst.BlockStmt{List: []dst.Stmt{&dst.ExprStmt{X: cloned}}},
			},
		},
	}

	restorer := decorator.NewRestorer()
	if _, err := restorer.RestoreFile(synthetic); err != nil {
		return "", ex.Wrapf(err, "failed to restore expression to source")
	}
	return RenderNode(restorer, cloned)
}

// RenderNode looks up node's restored counterpart in restorer and renders it
// back to Go source text.
func RenderNode(restorer *decorator.Restorer, node dst.Node) (string, error) {
	astNode, ok := restorer.Ast.Nodes[node]
	if !ok {
		return "", ex.New("failed to locate restored node")
	}
	var buf strings.Builder
	if err := format.Node(&buf, restorer.Fset, astNode); err != nil {
		return "", ex.Wrapf(err, "failed to format node")
	}
	return buf.String(), nil
}
