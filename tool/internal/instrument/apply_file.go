// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dave/dst"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/imports"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

// applyFileRule introduces the new file to the target package at compile time.
func (ip *instrumentPhase) applyFileRule(ctx context.Context, rule *rule.InstFileRule, pkgName string) error {
	file := filepath.Join(rule.ResolvedPath, rule.File)
	if !util.PathExists(file) {
		return ex.Newf("file %s not found in %s", rule.File, rule.ResolvedPath)
	}

	// Parse the new file into AST nodes and modify it as needed.
	// Keep processing in-memory to avoid mutating shared temp rule files.
	data, err := os.ReadFile(file)
	if err != nil {
		return ex.Wrapf(err, "reading rule source file %s", file)
	}
	root, err := ast.NewAstParser().ParseSource(ast.StripBuildIgnore(string(data)))
	if err != nil {
		return ex.Wrapf(err, "parsing rule source file %s", file)
	}
	// Always rename the package name to the target package name
	root.Name.Name = pkgName

	// Prefer imports discovered during setup so we do not re-parse the source
	// just for importcfg. Fall back to AST collection when SourceImports is empty
	// (e.g. unit tests that call applyFileRule directly).
	if err = ip.updateImportConfigForFileRule(ctx, root, rule); err != nil {
		return err
	}

	// Write back the modified AST to a new file in the working directory
	base := filepath.Base(rule.File)
	ext := filepath.Ext(base)
	newName := strings.TrimSuffix(base, ext)
	newFile := filepath.Join(ip.workDir, fmt.Sprintf("otelc.%s.go", newName))
	err = ast.WriteFile(newFile, root)
	if err != nil {
		return ex.Wrapf(err, "writing instrumented file %s", newFile)
	}
	ip.Info("Apply file rule", "rule", rule)

	// Add the new file as part of the source files to be compiled
	ip.addCompileArg(newFile)
	ip.keepForDebug(newFile)
	return nil
}

func (ip *instrumentPhase) updateImportConfigForFileRule(
	ctx context.Context,
	root *dst.File,
	rule *rule.InstFileRule,
) error {
	var pathMap map[string]string
	if len(rule.SourceImports) > 0 {
		pathMap = make(map[string]string, len(rule.SourceImports))
		for _, p := range rule.SourceImports {
			pathMap[p] = p
		}
	} else {
		// Fallback when SourceImports was not populated (e.g. direct unit tests).
		pathMap = imports.CollectPaths(ctx, root)
	}
	if len(pathMap) == 0 {
		return nil
	}
	if err := ip.updateImportConfig(ctx, pathMap); err != nil {
		return ex.Wrapf(err, "updating import config for file imports in %s", rule.Name)
	}
	return nil
}
