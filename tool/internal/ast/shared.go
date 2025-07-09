// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"fmt"

	"github.com/dave/dst"
)

func ListFuncDecls(file string) ([]*dst.FuncDecl, error) {
	// Parse the file to get only all the function declarations
	// So we can use fast variant of AST parsing
	parser := NewAstParser()
	root, parseErr := parser.ParseFileFast(file)
	if parseErr != nil {
		return nil, fmt.Errorf("failed to parse file %s: %w", file, parseErr)
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
