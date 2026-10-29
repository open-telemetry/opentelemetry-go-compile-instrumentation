// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"fmt"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/dave/dst"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/instrument"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

const (
	otelcRuntimeFile = "otelc.runtime.go"
)

// runtimePackage describes the package that receives a generated
// otelc.runtime.go: where it sits on disk, how it is imported, and the package
// name the generated file must declare.
type runtimePackage struct {
	dir        string
	importPath string
	debugPath  string
	name       string
}

//nolint:gochecknoglobals // This is a constant
var requiredImports = map[string]string{
	"runtime/debug": "_otel_debug", // The getstack function depends on runtime/debug
	"log":           "_otel_log",   // The printstack function depends on log
	"unsafe":        "_",           // The golinkname tag depends on unsafe
}

func genImportDecl(funcRules []*rule.InstFuncRule, fileRules []*rule.InstFileRule) []dst.Decl {
	var imports map[string]string
	if len(funcRules) > 0 {
		imports = maps.Clone(requiredImports) // clone required imports to avoid mutating the global map
		for _, m := range funcRules {
			imports[m.Path] = ast.IdentIgnore
		}
	} else {
		imports = make(map[string]string)
	}
	for _, m := range fileRules {
		imports[m.Path] = ast.IdentIgnore
	}
	importDecls := make([]dst.Decl, 0, len(imports))
	// Sort the keys to ensure deterministic order
	for _, k := range slices.Sorted(maps.Keys(imports)) {
		importDecls = append(importDecls, ast.ImportDecl(imports[k], k))
	}
	return importDecls
}

// genVarDecl declares the stack helper variables for each hook package and an
// init function that assigns them.
//
// The variables have no initializer, so each one is a BSS symbol. The linker
// accepts several BSS definitions of the same symbol, so two packages of one
// binary can both declare them. A variable with an initializer is a data
// symbol, and two data definitions fail the link with "duplicated definition
// of symbol".
func genVarDecl(matched []*rule.InstFuncRule) []dst.Decl {
	decls := make([]dst.Decl, 0, 2*len(matched)+1)
	var assigns []dst.Stmt
	uniquePath := map[string]bool{}
	for i, m := range matched {
		if _, ok := uniquePath[m.Path]; ok {
			continue
		}
		uniquePath[m.Path] = true
		// First variable declaration
		// //go:linkname _getstack%d %s.OtelGetStackImpl
		// var _getstack%d func() []byte
		getStackName := fmt.Sprintf("_getstack%d", i)
		getStackType := &dst.FuncType{
			Params: &dst.FieldList{},
			Results: &dst.FieldList{
				List: []*dst.Field{{Type: ast.ArrayType(ast.Ident("byte"))}},
			},
		}
		getStackVar := typedVarDecl(getStackName, getStackType)
		getStackVar.Decs = dst.GenDeclDecorations{
			NodeDecs: ast.LineComments(
				fmt.Sprintf("//go:linkname %s %s.OtelGetStackImpl", getStackName, m.Path)),
		}
		// Second variable declaration
		// //go:linkname _printstack%d %s.OtelPrintStackImpl
		// var _printstack%d func([]byte)
		printStackName := fmt.Sprintf("_printstack%d", i)
		printStackType := &dst.FuncType{
			Params: &dst.FieldList{
				List: []*dst.Field{{Type: ast.ArrayType(ast.Ident("byte"))}},
			},
		}
		printStackVar := typedVarDecl(printStackName, printStackType)
		printStackVar.Decs = dst.GenDeclDecorations{
			NodeDecs: ast.LineComments(
				fmt.Sprintf("//go:linkname %s %s.OtelPrintStackImpl", printStackName, m.Path)),
		}
		decls = append(decls, getStackVar, printStackVar)

		// build: _getstack%d = _otel_debug.Stack
		assigns = append(assigns, ast.AssignStmt(
			ast.Ident(getStackName),
			ast.SelectorExpr(ast.Ident("_otel_debug"), "Stack"),
		))
		// build: string(bt)
		stringCall := &dst.CallExpr{
			Fun:  ast.Ident("string"),
			Args: []dst.Expr{ast.Ident("bt")},
		}
		// build: _otel_log.Print(string(bt))
		printCall := &dst.CallExpr{
			Fun:  ast.SelectorExpr(ast.Ident("_otel_log"), "Print"),
			Args: []dst.Expr{stringCall},
		}
		// build: func (bt []byte) { _otel_log.Print(string(bt)) }
		printStackFunc := &dst.FuncLit{
			Type: &dst.FuncType{
				Params: &dst.FieldList{
					List: []*dst.Field{
						ast.Field("bt", ast.ArrayType(ast.Ident("byte"))),
					},
				},
			},
			Body: ast.BlockStmts(ast.ExprStmt(printCall)),
		}
		// build: _printstack%d = func (bt []byte) { _otel_log.Print(string(bt)) }
		assigns = append(assigns, ast.AssignStmt(ast.Ident(printStackName), printStackFunc))
	}
	if len(assigns) == 0 {
		return decls
	}
	initFunc := &dst.FuncDecl{
		Name: ast.Ident("init"),
		Type: &dst.FuncType{Params: &dst.FieldList{}},
		Body: ast.BlockStmts(assigns...),
	}
	return append(decls, initFunc)
}

func typedVarDecl(name string, typ dst.Expr) *dst.GenDecl {
	return &dst.GenDecl{
		Tok: token.VAR,
		Specs: []dst.Spec{
			&dst.ValueSpec{
				Names: []*dst.Ident{ast.Ident(name)},
				Type:  typ,
			},
		},
	}
}

func buildOtelcRuntimeAst(decls []dst.Decl, packageName string) *dst.File {
	const comment = "// This file is generated by the opentelemetry-go-compile-instrumentation tool. DO NOT EDIT."
	return &dst.File{
		Name: ast.Ident(packageName),
		Decs: dst.FileDecorations{
			NodeDecs: ast.LineComments(comment),
		},
		Decls: decls,
	}
}

type runtimeContributorID struct {
	kind, name, path, file, identity string
}

// runtimeContributors returns a sorted, deduplicated list of contributing rule names.
func runtimeContributors(funcRules []*rule.InstFuncRule, fileRules []*rule.InstFileRule) []string {
	var contributors []string
	seen := make(map[runtimeContributorID]bool)

	for _, r := range funcRules {
		// Distinct func rules are identified by their name and content identity.
		id := runtimeContributorID{kind: "func", name: r.GetName(), identity: r.Identity()}
		if seen[id] {
			continue
		}
		seen[id] = true
		contributors = append(contributors, firstNonEmpty(r.GetName(), r.Func, r.Path))
	}

	for _, r := range fileRules {
		// Distinct file rules are identified by their name, path, and file.
		id := runtimeContributorID{kind: "file", name: r.GetName(), path: r.Path, file: r.File}
		if seen[id] {
			continue
		}
		seen[id] = true
		contributors = append(contributors, firstNonEmpty(r.GetName(), r.File, r.Path))
	}

	slices.Sort(contributors)
	return contributors
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// writeRuntimeDiffForDebug writes the generated runtime file diff when debug output is enabled.
func (sp *setupPhase) writeRuntimeDiffForDebug(
	srcPath, pkgPath string,
	funcRules []*rule.InstFuncRule,
	fileRules []*rule.InstFileRule,
) {
	if !instrument.DiffDebugEnabled() {
		return
	}
	dest := filepath.Join(setupDebugDir(pkgPath), filepath.Base(srcPath)+".diff")
	header := instrument.FormatGeneratedFileHeader(otelcRuntimeFile, runtimeContributors(funcRules, fileRules))
	instrument.WriteAddedSourceDiff(dest, srcPath, header, sp.logger)
}

// removeRuntimeFile deletes the runtime file in pkgDir that an earlier setup
// generated from rules that no longer apply. A stale runtime file keeps its old
// imports and linkname declarations active in the build.
func (sp *setupPhase) removeRuntimeFile(ctx context.Context, pkgDir string) error {
	otelcRuntimeFilePath := filepath.Join(pkgDir, otelcRuntimeFile)
	if !util.PathExists(otelcRuntimeFilePath) {
		return nil
	}

	// Track the file first so that a later revert can restore it.
	if stateManager, found := stateManagerFromContext(ctx); found {
		if err := stateManager.Track(otelcRuntimeFilePath); err != nil {
			return err
		}
	}
	if err := os.Remove(otelcRuntimeFilePath); err != nil {
		return ex.Wrapf(err, "removing stale otelc runtime file %s", otelcRuntimeFilePath)
	}
	sp.Info("Removed stale otelc.runtime.go", "path", otelcRuntimeFilePath)

	return nil
}

// addDeps generates and writes otelc.runtime.go with required imports and variable
// declarations for OpenTelemetry instrumentation based on matched rules.
//
// Rules that keep their hook code in pkg itself are dropped, since a generated
// file cannot import the package it belongs to. `otelc go test ./...` selects
// such a package when the hooks live in the application module.
func (sp *setupPhase) addDeps(ctx context.Context, matched []*rule.InstRuleSet, pkg runtimePackage) error {
	var funcRules []*rule.InstFuncRule
	var fileRules []*rule.InstFileRule
	for _, m := range matched {
		for _, funcRule := range m.AllFuncRules() {
			if funcRule.Path != pkg.importPath {
				funcRules = append(funcRules, funcRule)
			}
		}
		for _, fileRule := range m.FileRules {
			if fileRule.Path != pkg.importPath {
				fileRules = append(fileRules, fileRule)
			}
		}
	}
	if len(funcRules) == 0 && len(fileRules) == 0 {
		return sp.removeRuntimeFile(ctx, pkg.dir)
	}

	// Add required imports
	importDecls := genImportDecl(funcRules, fileRules)
	// Generate the variable declarations that used by otel runtime
	varDecls := genVarDecl(funcRules)
	// build the ast
	root := buildOtelcRuntimeAst(append(importDecls, varDecls...), pkg.name)
	otelcRuntimeFilePath := filepath.Join(pkg.dir, otelcRuntimeFile)
	// Track file in state manager
	if stateManager, found := stateManagerFromContext(ctx); found {
		if err := stateManager.Track(otelcRuntimeFilePath); err != nil {
			return err
		}
	}
	// Write the ast to file
	if err := ast.WriteFileAtomic(otelcRuntimeFilePath, root); err != nil {
		return ex.Wrapf(err, "writing otelc runtime file %s", otelcRuntimeFilePath)
	}
	debugPath := pkg.debugPath
	if debugPath == "" {
		debugPath = pkg.importPath
	}
	keepForDebug(ctx, otelcRuntimeFilePath, debugPath)
	sp.writeRuntimeDiffForDebug(otelcRuntimeFilePath, debugPath, funcRules, fileRules)
	sp.Info("Created otelc.runtime.go", "path", otelcRuntimeFilePath)
	return nil
}
