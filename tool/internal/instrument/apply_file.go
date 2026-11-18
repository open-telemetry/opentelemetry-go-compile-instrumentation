// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	toolruntime "go.opentelemetry.io/otelc/tool/internal/runtime"
	"go.opentelemetry.io/otelc/tool/util"
)

// applyFileRule introduces the new file to the target package at compile time.
func (ip *instrumentPhase) applyFileRule(ctx context.Context, fileRule *rule.InstFileRule, pkgName string) error {
	if pkgName == "" {
		return ex.Newf("cannot resolve the package name for rule %q; refusing to add %s",
			fileRule.Name, fileRule.File)
	}
	file, data, err := readFileRuleSource(fileRule)
	if err != nil {
		return err
	}

	// Evaluate build constraints before adding the file to compilation.
	// go tool compile compiles all explicit CLI arguments unconditionally,
	// so we must skip files whose build constraints do not match the target platform.
	match, err := ast.MatchFileConstraints(ip.getBuildContext(), fileRule.ResolvedPath, fileRule.File, data)
	if err != nil {
		return ex.Wrapf(err, "matching build constraints for %s", file)
	}
	if !match {
		ip.Debug("File rule build constraints not satisfied; skipping", "rule", fileRule.Name, "file", fileRule.File)
		return nil
	}

	strippedSource := ast.StripBuildIgnoreTag(string(data))
	root, err := ast.NewAstParser().ParseSource(strippedSource)
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
	newFile := ip.addedFilePath(fileRule.File)
	err = ast.WriteFile(newFile, root)
	if err != nil {
		return ex.Wrapf(err, "writing instrumented file %s", newFile)
	}
	ip.Info("Apply file rule", "rule", fileRule)

	// Add the new file as part of the source files to be compiled
	ip.addCompileArg(newFile)
	ip.keepForDebug(newFile)
	ruleName := fileRule.Name
	if ruleName == "" {
		ruleName = fileRule.File
	}
	ip.writeFileRuleDiffForDebug(newFile, ruleName)
	return nil
}

// readFileRuleSource reads the source file of a file rule. It rejects paths
// that leave the rule package directory, both lexically and through symlinks.
// It returns the path of the file and its contents.
func readFileRuleSource(fileRule *rule.InstFileRule) (string, []byte, error) {
	if fileRule.Path == util.InternalPath {
		data, err := toolruntime.ReadInternalFile(fileRule.File)
		if err != nil {
			return "", nil, err
		}
		return fileRule.File, data, nil
	}
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

// addedFilePath returns where the file a rule adds from src is written in the
// work dir. The name is otelc.<name>.go, numbered when that is already taken
// by the globals file or by a file an earlier rule added to the package. Two
// rules can add files with the same base name from different directories, and
// writing both to one path would compile the last one twice and drop the other.
func (ip *instrumentPhase) addedFilePath(src string) string {
	base := filepath.Base(src)
	name := "otelc." + strings.TrimSuffix(base, filepath.Ext(base))
	path := filepath.Join(ip.workDir, name+".go")
	for i := 1; filepath.Base(path) == otelcGlobalsFile || slices.Contains(ip.compileArgs, path); i++ {
		path = filepath.Join(ip.workDir, fmt.Sprintf("%s.%d.go", name, i))
	}
	return path
}
