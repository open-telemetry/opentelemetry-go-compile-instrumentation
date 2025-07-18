// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"fmt"
	"go/token"

	"github.com/dave/dst"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
)

func ListFuncDecls(file string) ([]*dst.FuncDecl, error) {
	// Parse the file to get only all the function declarations
	// So we can use fast variant of AST parsing
	parser := NewAstParser()
	root, parseErr := parser.ParseFileFast(file)
	if parseErr != nil {
		return nil, ex.Errorf(parseErr, "failed to parse file %s", file)
	}
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

func NewImportDecl(alias, path string) *dst.GenDecl {
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
