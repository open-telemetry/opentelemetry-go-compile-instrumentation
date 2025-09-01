// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

type AstParser struct {
	fset *token.FileSet
	dec  *decorator.Decorator
}

func NewAstParser() *AstParser {
	return &AstParser{
		fset: token.NewFileSet(),
	}
}

// ParseFile parses the AST from a file.
func (ap *AstParser) Parse(filePath string, mode parser.Mode) (*dst.File, error) {
	util.Assert(ap.fset != nil, "fset is not initialized")

	name := filepath.Base(filePath)
	file, err := os.Open(filePath)
	if err != nil {
		return nil, ex.Errorf(err, "failed to open file %s", filePath)
	}
	defer file.Close()
	astFile, err := parser.ParseFile(ap.fset, name, file, mode)
	if err != nil {
		return nil, ex.Errorf(err, "failed to parse file %s", filePath)
	}
	ap.dec = decorator.NewDecorator(ap.fset)
	dstFile, err := ap.dec.DecorateFile(astFile)
	if err != nil {
		return nil, ex.Errorf(err, "failed to decorate file %s", filePath)
	}
	return dstFile, nil
}

// ParseSource parses the AST from complete source code.
func (ap *AstParser) ParseSource(source string) (*dst.File, error) {
	util.Assert(source != "", "empty source")
	ap.dec = decorator.NewDecorator(ap.fset)
	dstRoot, err := ap.dec.Parse(source)
	if err != nil {
		return nil, ex.Error(err)
	}
	return dstRoot, nil
}

func (ap *AstParser) FindPosition(node dst.Node) token.Position {
	astNode := ap.dec.Ast.Nodes[node]
	if astNode == nil {
		return token.Position{Filename: "", Line: -1, Column: -1} // Invalid
	}
	return ap.fset.Position(astNode.Pos())
}

func WriteFile(filePath string, root *dst.File) error {
	file, err := os.Create(filePath)
	if err != nil {
		return ex.Errorf(err, "failed to create file %s", filePath)
	}
	defer file.Close()
	r := decorator.NewRestorer()
	err = r.Fprint(file, root)
	if err != nil {
		return ex.Errorf(err, "failed to write to file %s", filePath)
	}
	return nil
}

// ParseFileOnlyPackage parses the AST from a file with the package clause only mode.
func ParseFileOnlyPackage(filePath string) (*dst.File, error) {
	return NewAstParser().Parse(filePath, parser.PackageClauseOnly)
}

// ParseFileFast parses the AST from a file with the skip object resolution mode.
func ParseFileFast(filePath string) (*dst.File, error) {
	return NewAstParser().Parse(filePath, parser.SkipObjectResolution)
}

// ParseFile parses the AST from a file with the parse comments mode. This is the
// default mode for most cases.
func ParseFile(filePath string) (*dst.File, error) {
	return NewAstParser().Parse(filePath, parser.ParseComments)
}
