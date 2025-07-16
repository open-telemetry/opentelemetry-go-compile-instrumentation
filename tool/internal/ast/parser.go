// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"fmt"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
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
		return nil, fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer file.Close()
	astFile, err := parser.ParseFile(ap.fset, name, file, mode)
	if err != nil {
		return nil, fmt.Errorf("failed to parse file %s: %w", filePath, err)
	}
	ap.dec = decorator.NewDecorator(ap.fset)
	dstFile, err := ap.dec.DecorateFile(astFile)
	if err != nil {
		return nil, fmt.Errorf("failed to decorate file %s: %w", filePath, err)
	}
	return dstFile, nil
}

func (ap *AstParser) ParseFileFast(filePath string) (*dst.File, error) {
	return ap.ParseFile(filePath, parser.SkipObjectResolution)
}

func WriteFile(filePath string, root *dst.File) error {
	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", filePath, err)
	}
	defer file.Close()
	fset := token.NewFileSet()
	restorer := decorator.NewRestorer()
	astFile, err := restorer.RestoreFile(root)
	if err != nil {
		return fmt.Errorf("failed to restore file %s: %w", filePath, err)
	}
	cfg := printer.Config{}
	err = cfg.Fprint(file, fset, astFile)
	if err != nil {
		return fmt.Errorf("failed to write to file %s: %w", filePath, err)
	}
	return nil
}
