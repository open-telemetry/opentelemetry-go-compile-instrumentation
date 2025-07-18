// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"go/parser"
	"go/printer"
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

func (ap *AstParser) ParseFile(filePath string, mode parser.Mode) (*dst.File, error) {
	util.Assert(ap.fset != nil, "fset is not initialized")

	name := filepath.Base(filePath)
	file, err := os.Open(filePath)
	if err != nil {
		return nil, ex.Error(err)
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

func (ap *AstParser) ParseFileFast(filePath string) (*dst.File, error) {
	return ap.ParseFile(filePath, parser.SkipObjectResolution)
}

func WriteFile(filePath string, root *dst.File) error {
	file, err := os.Create(filePath)
	if err != nil {
		return ex.Errorf(err, "failed to create file %s", filePath)
	}
	defer file.Close()
	fset := token.NewFileSet()
	restorer := decorator.NewRestorer()
	astFile, err := restorer.RestoreFile(root)
	if err != nil {
		return ex.Errorf(err, "failed to restore file %s", filePath)
	}
	cfg := printer.Config{}
	err = cfg.Fprint(file, fset, astFile)
	if err != nil {
		return ex.Errorf(err, "failed to write to file %s", filePath)
	}
	return nil
}
