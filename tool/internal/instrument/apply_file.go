// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/ast"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

func listRuleFiles(path string) ([]string, error) {
	// If the path is a local path, use it directly, otherwise, it's a module
	// path, we need to convert it to a local path and find the module files
	var p string
	if util.PathExists(path) {
		p = path
	} else {
		p = strings.TrimPrefix(path, util.OtelRoot)
		p = filepath.Join(util.GetBuildTempDir(), p)
	}
	return util.ListFiles(p)
}

// applyFileRule introduces the new file to the target package at compile time.
func (ip *InstrumentPhase) applyFileRule(rule *rule.InstFileRule, pkgName string) error {
	// List all files in the rule module path
	files, err := listRuleFiles(rule.Path)
	if err != nil {
		return err
	}

	// Find the new file we want to introduce
	index := slices.IndexFunc(files, func(file string) bool {
		return strings.HasSuffix(file, rule.File)
	})
	if index == -1 {
		return ex.Newf("file %s not found", rule.File)
	}
	file := files[index]

	// Parse the new file into AST nodes and modify it as needed
	root, err := ip.parseFile(file)
	if err != nil {
		return err
	}
	// Always rename the package name to the target package name
	root.Name.Name = pkgName

	// The file being added has its own imports that need to be in importcfg.
	// Without this, the compiler will fail with "could not import X" errors.
	if err = ip.updateImportConfigForFile(root, rule.Name); err != nil {
		return err
	}

	// Add any rule-specified additional imports if defined.
	if err = ip.handleRuleImports(root, rule.Imports, rule.Name, "file rule"); err != nil {
		return err
	}

	// Write back the modified AST to a new file in the working directory
	base := filepath.Base(rule.File)
	ext := filepath.Ext(base)
	newName := strings.TrimSuffix(base, ext)
	newFile := filepath.Join(ip.workDir, fmt.Sprintf("otel.%s.go", newName))
	err = ast.WriteFile(newFile, root)
	if err != nil {
		return err
	}
	ip.Info("Apply file rule", "rule", rule)

	// Add the new file as part of the source files to be compiled
	ip.addCompileArg(newFile)
	ip.keepForDebug(newFile)
	return nil
}
