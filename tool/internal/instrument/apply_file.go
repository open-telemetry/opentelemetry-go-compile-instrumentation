// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"errors"
	"fmt"
	"go/build/constraint"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
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
func (ip *instrumentPhase) applyFileRule(ctx context.Context, fileRule *rule.InstFileRule, pkgName string) error {
	file, data, err := readFileRuleSource(fileRule)
	if err != nil {
		return err
	}
	root, err := ast.NewAstParser().ParseSource(stripBuildIgnoreTag(string(data)))
	if err != nil {
		return ex.Wrapf(err, "parsing rule source file %s", file)
	}
	// Always rename the package name to the target package name
	root.Name.Name = pkgName

	// The file being added has its own imports that need to be in importcfg.
	// Without this, the compiler will fail with "could not import X" errors.
	if err = ip.updateImportConfigForFile(ctx, root, fileRule.Name); err != nil {
		return err
	}

	// Write back the modified AST to a new file in the working directory
	base := filepath.Base(fileRule.File)
	ext := filepath.Ext(base)
	newName := strings.TrimSuffix(base, ext)
	newFile := filepath.Join(ip.workDir, fmt.Sprintf("otelc.%s.go", newName))
	err = ast.WriteFile(newFile, root)
	if err != nil {
		return ex.Wrapf(err, "writing instrumented file %s", newFile)
	}
	ip.Info("Apply file rule", "rule", fileRule)

	// Add the new file as part of the source files to be compiled
	ip.addCompileArg(newFile)
	ip.keepForDebug(newFile)
	return nil
}

// readFileRuleSource reads the source file of a file rule. It rejects paths
// that leave the rule package directory, both lexically and through symlinks.
// It returns the path of the file and its contents.
func readFileRuleSource(fileRule *rule.InstFileRule) (string, []byte, error) {
	if strings.TrimSpace(fileRule.ResolvedPath) == "" {
		return "", nil, ex.Newf("resolved path for rule %q is empty", fileRule.Name)
	}
	if strings.TrimSpace(fileRule.File) == "" {
		return "", nil, ex.Newf("file for rule %q is empty", fileRule.Name)
	}
	if rule.IsDisallowedFilePath(fileRule.File) {
		return "", nil, ex.Newf("file %q for rule %q must be a relative path", fileRule.File, fileRule.Name)
	}

	absBase, err := filepath.Abs(fileRule.ResolvedPath)
	if err != nil {
		return "", nil, ex.Wrapf(err, "resolving absolute path for rule directory %s", fileRule.ResolvedPath)
	}

	file := filepath.Join(absBase, fileRule.File)

	// Lexical containment check: ensure candidate path does not escape absBase.
	lexRel, err := filepath.Rel(absBase, file)
	if err != nil || lexRel == ".." || strings.HasPrefix(lexRel, ".."+string(filepath.Separator)) {
		return "", nil, ex.Newf("file %s is outside rule package directory %s", fileRule.File, fileRule.ResolvedPath)
	}

	// Root access ensures the file read stays within absBase. It follows only
	// contained relative symlinks and rejects absolute or escaping symlink
	// targets. On supported platforms it also keeps the opened root stable if
	// the directory is renamed while the rule is applied.
	ruleRoot, err := os.OpenRoot(absBase)
	if err != nil {
		return "", nil, ex.Wrapf(err, "opening rule package directory %s", fileRule.ResolvedPath)
	}
	defer func() { _ = ruleRoot.Close() }()

	// Read into memory so later processing never mutates shared temp rule files.
	data, err := ruleRoot.ReadFile(fileRule.File)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil, ex.Newf("file %s not found in %s", fileRule.File, fileRule.ResolvedPath)
		}
		return "", nil, ex.Wrapf(err, "reading rule source file %s", file)
	}
	return file, data, nil
}
