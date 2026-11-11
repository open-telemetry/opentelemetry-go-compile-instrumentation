// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	toolruntime "go.opentelemetry.io/otelc/tool/internal/runtime"
	"go.opentelemetry.io/otelc/tool/util"
)

type fileRuleSource struct {
	data  []byte
	dir   string
	label string
}

func readFileRuleSource(r *rule.InstFileRule) (fileRuleSource, error) {
	if r.Path == util.InternalPath {
		data, err := toolruntime.ReadInternalFile(r.File)
		if err != nil {
			return fileRuleSource{}, err
		}
		return fileRuleSource{data: data, label: r.File}, nil
	}
	file := filepath.Join(r.ResolvedPath, r.File)
	if !util.PathExists(file) {
		return fileRuleSource{}, ex.Newf("file %s not found in %s", r.File, r.ResolvedPath)
	}
	// Keep processing in-memory to avoid mutating shared temp rule files.
	data, err := os.ReadFile(file)
	if err != nil {
		return fileRuleSource{}, ex.Wrapf(err, "reading rule source file %s", file)
	}
	return fileRuleSource{data: data, dir: r.ResolvedPath, label: file}, nil
}

func (ip *instrumentPhase) applyFileRule(ctx context.Context, rule *rule.InstFileRule, pkgName string) error {
	if pkgName == "" {
		return ex.Newf("cannot resolve the package name for rule %q; refusing to add %s",
			rule.Name, rule.File)
	}

	src, err := readFileRuleSource(rule)
	if err != nil {
		return err
	}

	// Evaluate build constraints before adding the file to compilation.
	// go tool compile compiles all explicit CLI arguments unconditionally,
	// so we must skip files whose build constraints do not match the target platform.
	match, err := ast.MatchFileConstraints(ip.getBuildContext(), src.dir, rule.File, src.data)
	if err != nil {
		return ex.Wrapf(err, "matching build constraints for %s", src.label)
	}
	if !match {
		ip.Debug("File rule build constraints not satisfied; skipping", "rule", rule.Name, "file", rule.File)
		return nil
	}

	strippedSource := ast.StripBuildIgnoreTag(string(src.data))
	root, err := ast.NewAstParser().ParseSource(strippedSource)
	if err != nil {
		return ex.Wrapf(err, "parsing rule source file %s", src.label)
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
	ruleName := rule.Name
	if ruleName == "" {
		ruleName = rule.File
	}
	ip.writeFileRuleDiffForDebug(newFile, ruleName)
	return nil
}
