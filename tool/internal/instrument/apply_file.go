// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"fmt"
	"go/build/constraint"
	"os"
	"path/filepath"
	"strings"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

// stripBuildIgnoreTag removes genuine "//go:build ignore" constraint lines
// from content, line by line. It leaves every other occurrence of that text —
// inside a string literal, inside comment prose, anywhere that isn't itself a
// build-constraint line — untouched. See #1069: a whole-file substring
// replace corrupted both of those.
func stripBuildIgnoreTag(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if constraint.IsGoBuild(line) {
			lines[i] = ""
		}
	}
	return strings.Join(lines, "\n")
}

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
	root, err := ast.NewAstParser().ParseSource(stripBuildIgnoreTag(string(data)))
	if err != nil {
		return ex.Wrapf(err, "parsing rule source file %s", file)
	}
	// Always rename the package name to the target package name
	root.Name.Name = pkgName

	// The file being added has its own imports that need to be in importcfg.
	// Without this, the compiler will fail with "could not import X" errors.
	if err = ip.updateImportConfigForFile(ctx, root, rule.Name); err != nil {
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
