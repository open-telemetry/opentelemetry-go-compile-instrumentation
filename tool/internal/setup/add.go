// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/dave/dst"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/imports"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

const (
	OtelcRuntimeFile = "otelc.runtime.go"
)

//nolint:gochecknoglobals // This is a constant
var requiredImports = map[string]string{
	"runtime/debug": "_otel_debug", // The getstack function depends on runtime/debug
	"log":           "_otel_log",   // The printstack function depends on log
	"unsafe":        "_",           // The golinkname tag depends on unsafe
}

// registerBlankImport records path as a blank import in otelc.runtime.go so
// go build includes it in the action graph before toolexec runs.
func registerBlankImport(dst map[string]string, path string) {
	if path == "" || path == "C" {
		return
	}
	dst[path] = ast.IdentIgnore
}

func registerRuleImports(dst, ruleImports map[string]string) {
	for _, path := range ruleImports {
		registerBlankImport(dst, path)
	}
}

func collectRuleSetBaseImports(dst map[string]string, m *rule.InstRuleSet) {
	for _, rs := range m.RawRules {
		for _, r := range rs {
			registerRuleImports(dst, r.Imports)
		}
	}
	for _, rs := range m.StructRules {
		for _, r := range rs {
			registerRuleImports(dst, r.Imports)
		}
	}
	for _, rs := range m.CallRules {
		for _, r := range rs {
			registerRuleImports(dst, r.Imports)
		}
	}
	for _, rs := range m.DirectiveRules {
		for _, r := range rs {
			registerRuleImports(dst, r.Imports)
		}
	}
	for _, rs := range m.DeclRules {
		for _, r := range rs {
			registerRuleImports(dst, r.Imports)
		}
	}
}

// collectRuntimeImports gathers every import that instrumentation may introduce
// into a compile unit. go build finalizes its action graph before toolexec, so
// these paths must appear as blank imports in otelc.runtime.go.
func collectRuntimeImports(matched []*rule.InstRuleSet) (map[string]string, []*rule.InstFuncRule, error) {
	importsMap := make(map[string]string)
	funcRules := make([]*rule.InstFuncRule, 0)

	for _, m := range matched {
		for _, r := range m.AllFuncRules() {
			funcRules = append(funcRules, r)
			registerBlankImport(importsMap, r.Path)
			registerRuleImports(importsMap, r.Imports)
		}
		for _, r := range m.FileRules {
			registerBlankImport(importsMap, r.Path)
			registerRuleImports(importsMap, r.Imports)
			if err := collectFileRuleSourceImports(importsMap, r); err != nil {
				return nil, nil, err
			}
		}
		collectRuleSetBaseImports(importsMap, m)
	}

	if len(funcRules) > 0 {
		maps.Copy(importsMap, requiredImports)
	}

	return importsMap, funcRules, nil
}

// collectFileRuleSourceImports parses the add_file source (often //go:build ignore)
// and blank-registers its imports. Blank-importing rule.Path alone only pulls the
// stub package into the build graph, not dependencies of the ignored implementation.
func collectFileRuleSourceImports(dst map[string]string, r *rule.InstFileRule) error {
	if r.ResolvedPath == "" || r.File == "" {
		return nil
	}

	file := filepath.Join(r.ResolvedPath, r.File)
	if !util.PathExists(file) {
		return ex.Newf("file %s not found in %s for rule %s", r.File, r.ResolvedPath, r.Name)
	}

	data, err := os.ReadFile(file)
	if err != nil {
		return ex.Wrapf(err, "reading rule source file %s", file)
	}

	root, err := ast.NewAstParser().ParseSource(ast.StripBuildIgnore(string(data)))
	if err != nil {
		return ex.Wrapf(err, "parsing rule source file %s", file)
	}

	for _, path := range imports.Paths(root) {
		registerBlankImport(dst, path)
	}
	return nil
}

func genImportDecl(importsMap map[string]string) []dst.Decl {
	if len(importsMap) == 0 {
		return nil
	}
	importDecls := make([]dst.Decl, 0, len(importsMap))
	for _, k := range slices.Sorted(maps.Keys(importsMap)) {
		importDecls = append(importDecls, ast.ImportDecl(importsMap[k], k))
	}
	return importDecls
}

func genVarDecl(matched []*rule.InstFuncRule) []dst.Decl {
	decls := make([]dst.Decl, 0, len(matched))
	uniquePath := map[string]bool{}
	for i, m := range matched {
		if _, ok := uniquePath[m.Path]; ok {
			continue
		}
		uniquePath[m.Path] = true
		// First variable declaration
		// //go:linkname _getstack%d %s.OtelGetStackImpl
		// var _getstack%d = _otel_debug.Stack
		value := ast.SelectorExpr(ast.Ident("_otel_debug"), "Stack")
		getStackVar := ast.VarDecl(fmt.Sprintf("_getstack%d", i), value)
		getStackVar.Decs = dst.GenDeclDecorations{
			NodeDecs: ast.LineComments(
				fmt.Sprintf("//go:linkname _getstack%d %s.OtelGetStackImpl", i, m.Path)),
		}
		// Second variable declaration
		// //go:linkname _printstack%d %s.OtelPrintStackImpl
		// var _printstack%d = func (bt []byte){ _otel_log.Print(string(bt)) }
		// Build: string(bt)
		stringCall := &dst.CallExpr{
			Fun:  ast.Ident("string"),
			Args: []dst.Expr{ast.Ident("bt")},
		}
		// Build: _otel_log.Print(string(bt))
		printCall := &dst.CallExpr{
			Fun:  ast.SelectorExpr(ast.Ident("_otel_log"), "Print"),
			Args: []dst.Expr{stringCall},
		}
		// Build: func (bt []byte) { _otel_log.Print(string(bt)) }
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
		printStackVar := ast.VarDecl(fmt.Sprintf("_printstack%d", i), printStackFunc)
		printStackVar.Decs = dst.GenDeclDecorations{
			NodeDecs: ast.LineComments(
				fmt.Sprintf("//go:linkname _printstack%d %s.OtelPrintStackImpl", i, m.Path)),
		}
		decls = append(decls, getStackVar, printStackVar)
	}
	return decls
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

// addDeps generates and writes otelc.runtime.go with required imports and variable
// declarations for OpenTelemetry instrumentation based on matched rules.
func (sp *SetupPhase) addDeps(
	ctx context.Context,
	importsMap map[string]string,
	funcRules []*rule.InstFuncRule,
	packagePath, packageName string,
) error {
	importDecls := genImportDecl(importsMap)
	varDecls := genVarDecl(funcRules)
	if len(importDecls) == 0 && len(varDecls) == 0 {
		return nil
	}

	root := buildOtelcRuntimeAst(append(importDecls, varDecls...), packageName)
	otelcRuntimeFilePath := filepath.Join(packagePath, OtelcRuntimeFile)
	// Track file in state manager
	if stateManager, found := StateManagerFromContext(ctx); found {
		if err := stateManager.Track(otelcRuntimeFilePath); err != nil {
			return err
		}
	}
	// Write the ast to file
	if err := ast.WriteFileAtomic(otelcRuntimeFilePath, root); err != nil {
		return ex.Wrapf(err, "writing otelc runtime file %s", otelcRuntimeFilePath)
	}
	keepForDebug(ctx, otelcRuntimeFilePath)
	sp.Info("Created otelc.runtime.go", "path", otelcRuntimeFilePath)
	return nil
}
