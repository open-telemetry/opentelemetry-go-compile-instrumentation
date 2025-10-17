// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"fmt"
	"go/parser"
	"path/filepath"
	"slices"
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/ast"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

func listRuleFiles(p string) ([]string, error) {
	var path string
	if util.PathExists(p) {
		path = p
	} else {
		path = strings.TrimPrefix(p, util.OtelRoot)
		path = filepath.Join(util.GetBuildTempDir(), path)
	}
	files, err := util.ListFiles(path)
	if err != nil {
		return nil, err
	}
	return files, nil
}

// applyFileRule introduces the new file to the target package at compile time.
func (ip *InstrumentPhase) applyFileRule(rule *rule.InstFileRule) error {
	util.Assert(rule.File != "", "sanity check")
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
	p := ast.NewAstParser()
	root, err := p.Parse(file, parser.ParseComments)
	if err != nil {
		return err
	}
	// Always rename the package name to the target package name
	root.Name.Name = ip.packageName

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
