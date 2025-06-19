// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal

import (
	"go/parser"
	"go/token"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
)

const (
	TargetPkg      = "main"
	TargetFunc     = "main"
	TrampolineName = "Trampoline"
	HookName       = "Hook"
)

func loadAst(_ *slog.Logger, filePath string) *dst.File {
	name := filepath.Base(filePath)
	fset := token.NewFileSet()
	file, err := os.Open(filePath)
	if err != nil {
		panic(err)
	}
	astFile, err := parser.ParseFile(fset, name, file, parser.ParseComments)
	if err != nil {
		panic(err)
	}
	dec := decorator.NewDecorator(fset)
	dstFile, err := dec.DecorateFile(astFile)
	if err != nil {
		panic(err)
	}
	return dstFile
}

func storeAst(logger *slog.Logger, filePath string, ast *dst.File) {
	f, err := os.Create(filePath)
	if err != nil {
		panic(err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			logger.Error("Failed to close file", "path", filePath)
		}
	}()
	r := decorator.NewRestorer()
	err = r.Fprint(f, ast)
	if err != nil {
		panic(err)
	}
}

func newTrampolineFunc() *dst.FuncDecl {
	trampoline := &dst.FuncDecl{
		Name: &dst.Ident{
			Name: TrampolineName,
		},
		Type: &dst.FuncType{},
		Body: &dst.BlockStmt{
			List: []dst.Stmt{newFuncCall(HookName)},
		},
	}
	return trampoline
}

func newFuncCall(target string) *dst.ExprStmt {
	return &dst.ExprStmt{
		X: &dst.CallExpr{
			Fun: &dst.Ident{
				Name: target,
			},
		},
	}
}

func newHookFunc(name string) *dst.FuncDecl {
	return &dst.FuncDecl{
		Name: &dst.Ident{
			Name: name,
		},
		Type: &dst.FuncType{
			Params: &dst.FieldList{},
		},
	}
}

func findOutputDir(args []string) string {
	for i, arg := range args {
		if arg == "-o" {
			return filepath.Dir(args[i+1])
		}
	}
	return ""
}

func rewriteAst(logger *slog.Logger, ast *dst.File, fn *dst.FuncDecl) {
	logger.Info("Instrumenting function", "function", fn.Name.Name)
	// Insert a call to the trampoline function at the beginning of the function
	callToTrampoline := newFuncCall(TrampolineName)
	fn.Body.List = append([]dst.Stmt{callToTrampoline}, fn.Body.List...)
	// Add the trampoline function to the AST
	ast.Decls = append(ast.Decls, newTrampolineFunc())
	// Add the hook function to the AST
	hook := newHookFunc(HookName)
	ast.Decls = append(ast.Decls, hook)
}

func Instrument(logger *slog.Logger, args []string) []string {
	for i, arg := range args {
		if strings.HasSuffix(arg, ".go") {
			ast := loadAst(logger, arg)
			for _, decl := range ast.Decls {
				// Find target function
				fn, ok := decl.(*dst.FuncDecl)
				if !ok {
					continue
				}
				if fn.Name.Name != TargetFunc {
					continue
				}
				// Instrument the function
				rewriteAst(logger, ast, fn)
				// Update the compilation command
				modified := filepath.Join(findOutputDir(args), "modified.go")
				storeAst(logger, modified, ast)
				args[i] = modified
				break
			}
		}
	}

	// Remove the -complete flag because we added a body-less hook function
	// and the compiler will complain about it
	for i, arg := range args {
		if arg == "-complete" {
			args = append(args[:i], args[i+1:]...)
			break
		}
	}
	return args
}
