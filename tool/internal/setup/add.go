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
	otelcRuntimeFile     = "otelc.runtime.go"
	otelcRuntimeTestFile = "otelc.runtime_test.go"
)

// runtimePackage describes the package that receives a generated
// otelc.runtime.go: where it sits on disk, how it is imported, and the package
// name the generated file must declare.
type runtimePackage struct {
	dir        string
	importPath string
	debugPath  string
	name       string
	// importedByHooks holds the hook packages that import this package,
	// directly or through a chain. A generated file here cannot import them
	// back without creating an import cycle.
	importedByHooks map[string]bool
	testMode        bool
	fileTarget      bool
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

// addRuntimeTestDeps generates a test-only external package file that keeps
// hooks linked without adding a cyclic import to the package under test.
func (sp *setupPhase) addRuntimeTestDeps(
	ctx context.Context,
	pkg runtimePackage,
	hookPaths []string,
) error {
	if len(hookPaths) == 0 {
		return sp.removeRuntimeTestFile(ctx, pkg.dir)
	}

	if pkg.testMode && pkg.fileTarget {
		if err := sp.removeRuntimeTestFile(ctx, pkg.dir); err != nil {
			return err
		}
		return ex.Newf(
			"cannot keep hook packages %v linked when testing named Go files in %s; use a package pattern instead",
			hookPaths, pkg.dir,
		)
	}

	decls := make([]dst.Decl, 0, len(hookPaths))
	for _, hookPath := range hookPaths {
		decls = append(decls, ast.ImportDecl(ast.IdentIgnore, hookPath))
	}

	root := buildOtelcRuntimeAst(decls, pkg.name+"_test")
	filePath := filepath.Join(pkg.dir, otelcRuntimeTestFile)

	if stateManager, found := stateManagerFromContext(ctx); found {
		if err := stateManager.Track(filePath); err != nil {
			return err
		}
	}

	if err := ast.WriteFileAtomic(filePath, root); err != nil {
		return ex.Wrapf(err, "writing otelc runtime test file %s", filePath)
	}

	keepForDebug(ctx, filePath, pkg.importPath)
	sp.Info("Created otelc runtime test file", "path", filePath)
	return nil
}

// removeRuntimeFile deletes the runtime file in pkgDir that an earlier setup
// generated from rules that no longer apply. A stale runtime file keeps its old
// imports and linkname declarations active in the build.
func (sp *setupPhase) removeRuntimeFile(ctx context.Context, pkgDir string) error {
	return sp.removeGeneratedRuntimeFile(ctx, pkgDir, otelcRuntimeFile)
}

func (sp *setupPhase) removeRuntimeTestFile(ctx context.Context, pkgDir string) error {
	return sp.removeGeneratedRuntimeFile(ctx, pkgDir, otelcRuntimeTestFile)
}

func (sp *setupPhase) removeGeneratedRuntimeFile(
	ctx context.Context,
	pkgDir string,
	fileName string,
) error {
	filePath := filepath.Join(pkgDir, fileName)
	if !util.PathExists(filePath) {
		return nil
	}

	// Track the file first so a later revert can restore it.
	if stateManager, found := stateManagerFromContext(ctx); found {
		if err := stateManager.Track(filePath); err != nil {
			return err
		}
	}

	if err := os.Remove(filePath); err != nil {
		return ex.Wrapf(err, "removing stale generated runtime file %s", filePath)
	}

	sp.Info("Removed stale generated runtime file", "path", filePath)
	return nil
}

func recordSkippedHook(
	skippedHooks map[string]bool,
	hookPath string,
	pkg runtimePackage,
) {
	if hookPath != pkg.importPath && pkg.importedByHooks[hookPath] {
		skippedHooks[hookPath] = true
	}
}

func collectFunctionRules(
	matched []*rule.InstRuleSet,
	pkg runtimePackage,
) ([]*rule.InstFuncRule, map[string]bool) {
	funcRules := []*rule.InstFuncRule{}
	skippedHooks := make(map[string]bool)

	for _, m := range matched {
		for _, funcRule := range m.AllFuncRules() {
			if skipHook(funcRule.Path, pkg) {
				recordSkippedHook(skippedHooks, funcRule.Path, pkg)
				continue
			}
			funcRules = append(funcRules, funcRule)
		}
	}

	return funcRules, skippedHooks
}

func collectFileRules(
	matched []*rule.InstRuleSet,
	pkg runtimePackage,
	skippedHooks map[string]bool,
) []*rule.InstFileRule {
	fileRules := []*rule.InstFileRule{}

	for _, m := range matched {
		for _, fileRule := range m.FileRules {
			if skipHook(fileRule.Path, pkg) {
				recordSkippedHook(skippedHooks, fileRule.Path, pkg)
				continue
			}
			fileRules = append(fileRules, fileRule)
		}
	}

	return fileRules
}

// addDeps generates and writes otelc.runtime.go with required imports and variable
// declarations for OpenTelemetry instrumentation based on matched rules.
//
// Hooks whose packages import pkg cannot be imported by pkg's generated
// runtime file without creating an import cycle. During `otelc go test ./...`,
// those hooks are linked through a generated external test package instead.
func (sp *setupPhase) addDeps(
	ctx context.Context,
	matched []*rule.InstRuleSet,
	pkg runtimePackage,
) error {
	funcRules, skippedHooks := collectFunctionRules(matched, pkg)
	fileRules := collectFileRules(matched, pkg, skippedHooks)

	// Keep hooks that cannot be imported by the runtime file linked into
	// test binaries through a generated external test package.
	hookPaths := slices.Sorted(maps.Keys(skippedHooks))
	if err := sp.addRuntimeTestDeps(ctx, pkg, hookPaths); err != nil {
		return err
	}

	if len(funcRules) == 0 && len(fileRules) == 0 {
		return sp.removeRuntimeFile(ctx, pkg.dir)
	}

	// Add required imports.
	importDecls := genImportDecl(funcRules, fileRules)

	// Generate the variable declarations used by the runtime.
	varDecls := genVarDecl(funcRules)

	root := buildOtelcRuntimeAst(append(importDecls, varDecls...), pkg.name)
	otelcRuntimeFilePath := filepath.Join(pkg.dir, otelcRuntimeFile)

	// Track the file in the state manager.
	if stateManager, found := stateManagerFromContext(ctx); found {
		if err := stateManager.Track(otelcRuntimeFilePath); err != nil {
			return err
		}
	}

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

// skipHook reports whether the hook at hookPath must be left out of pkg's
// runtime file to avoid an import cycle.
func skipHook(hookPath string, pkg runtimePackage) bool {
	return hookPath == pkg.importPath || pkg.importedByHooks[hookPath]
}
