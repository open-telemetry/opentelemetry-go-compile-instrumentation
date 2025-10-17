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
	fset := token.NewFileSet()
	return &AstParser{
		fset: fset,
		dec:  decorator.NewDecorator(fset),
	}
}

// ParseFile parses the AST from a file.
func (ap *AstParser) Parse(filePath string, mode parser.Mode) (*dst.File, error) {
	util.Assert(ap.fset != nil, "fset is not initialized")

	name := filepath.Base(filePath)
	file, err := os.Open(filePath)
	if err != nil {
		return nil, ex.Wrapf(err, "failed to open file %s", filePath)
	}
	defer file.Close()
	astFile, err := parser.ParseFile(ap.fset, name, file, mode)
	if err != nil {
		return nil, ex.Wrapf(err, "failed to parse file %s", filePath)
	}
	dstFile, err := ap.dec.DecorateFile(astFile)
	if err != nil {
		return nil, ex.Wrapf(err, "failed to decorate file %s", filePath)
	}
	return dstFile, nil
}

// ParseSnippet parses the AST from incomplete source code snippet.
func (ap *AstParser) ParseSnippet(source string) ([]dst.Stmt, error) {
	if source == "" {
		return nil, ex.New("empty source")
	}
	snippet := "package main; func _() {" + source + "}"
	file, err := decorator.ParseFile(ap.fset, "", snippet, 0)
	if err != nil {
		return nil, ex.Wrap(err)
	}
	funcDecl, ok := file.Decls[0].(*dst.FuncDecl)
	util.Assert(ok, "must be a func decl")
	return funcDecl.Body.List, nil
}

// ParseSource parses the AST from complete source code.
func (ap *AstParser) ParseSource(source string) (*dst.File, error) {
	if source == "" {
		return nil, ex.New("empty source")
	}
	dstRoot, err := ap.dec.Parse(source)
	if err != nil {
		return nil, ex.Wrap(err)
	}
	return dstRoot, nil
}

// FindPosition finds the source position of a node in the AST.
func (ap *AstParser) FindPosition(node dst.Node) token.Position {
	astNode := ap.dec.Ast.Nodes[node]
	if astNode == nil {
		return token.Position{Filename: "", Line: -1, Column: -1} // Invalid
	}
	return ap.fset.Position(astNode.Pos())
}

// WriteFile writes the AST to a file.
func WriteFile(filePath string, root *dst.File) error {
	file, err := os.Create(filePath)
	if err != nil {
		return ex.Wrapf(err, "failed to create file %s", filePath)
	}
	defer file.Close()
	r := decorator.NewRestorer()
	err = r.Fprint(file, root)
	if err != nil {
		return ex.Wrapf(err, "failed to write to file %s", filePath)
	}
	return nil
}

// ParseFileOnlyPackage parses the AST from a file. Use it if you only need to
// read the package name from the AST.
func ParseFileOnlyPackage(filePath string) (*dst.File, error) {
	return NewAstParser().Parse(filePath, parser.PackageClauseOnly)
}

// ParseFileFast parses the AST from a file. Use this version if you only need
// to read information from the AST without writing it back to a file.
func ParseFileFast(filePath string) (*dst.File, error) {
	return NewAstParser().Parse(filePath, parser.SkipObjectResolution)
}

// ParseFile parses the AST from a file. Use this standard version if you need to
// write the AST back to a file, otherwise use ParseFileFast for better performance.
func ParseFile(filePath string) (*dst.File, error) {
	return NewAstParser().Parse(filePath, parser.ParseComments)
}
