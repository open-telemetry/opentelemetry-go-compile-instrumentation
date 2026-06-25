// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"errors"
	"go/parser"
	"path/filepath"
	"strconv"

	"golang.org/x/tools/go/packages"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/ast"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/pkgload"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

const (
	// Allowed names for the instrumentation config file.
	ToolFileCanonical = "otel.instrumentation.go"
	ToolFileAlias     = "otelc.tool.go"
)

type InstrumentationConfig struct {
	ImportPath string
	ToolFile   string
	RuleFiles  []string
}

//nolint:forbidigo // sentinel error; must not carry mutable stack state
var ErrNotInstrumentation = errors.New("not an instrumentation package")

func findToolFile(moduleDir string) (string, error) {
	canonical := filepath.Join(moduleDir, ToolFileCanonical)
	alias := filepath.Join(moduleDir, ToolFileAlias)

	canonicalExists := util.PathExists(canonical)
	aliasExists := util.PathExists(alias)

	switch {
	case canonicalExists && aliasExists:
		return "", ex.Newf(
			"both %q and %q exist; only one instrumentation config file is allowed",
			ToolFileCanonical,
			ToolFileAlias,
		)
	case canonicalExists:
		return canonical, nil
	case aliasExists:
		return alias, nil
	default:
		return "", nil
	}
}

func findToolFiles(moduleDirs map[string]bool) ([]string, error) {
	toolFiles := make([]string, 0, len(moduleDirs))
	for dir := range moduleDirs {
		toolFile, err := findToolFile(dir)
		if err != nil {
			return nil, err
		}
		if toolFile != "" {
			toolFiles = append(toolFiles, toolFile)
		}
	}
	return toolFiles, nil
}

func resolveInstrumentationConfig(ctx context.Context, dir, importPath string) (*InstrumentationConfig, error) {
	pkgs, loadErr := packages.Load(&packages.Config{
		Mode:    packages.NeedFiles | packages.NeedModule,
		Context: ctx,
		Dir:     dir,
	}, importPath)
	if loadErr != nil {
		return nil, ex.Wrapf(loadErr, "failed to load package for import %s", importPath)
	}

	if len(pkgs) != 1 {
		return nil, ex.Newf("expected exactly one package for %s, got %d", importPath, len(pkgs))
	}

	pkg := pkgs[0]
	if len(pkg.Errors) > 0 {
		return nil, ex.Newf("errors loading package %s: %v", importPath, pkg.Errors)
	}

	if pkg.Module == nil {
		return nil, ex.Newf("package %s is not part of a module", importPath)
	}

	modDir := pkg.Module.Dir
	pkgDir := pkgload.GetPackageDir(pkg)
	if pkgDir == "" {
		return nil, ex.Newf("could not determine directory for package %s", importPath)
	}

	// Always look for tool file in the module directory
	toolFile, findErr := findToolFile(modDir)
	if findErr != nil {
		return nil, ex.Wrapf(findErr, "checking for tool file in instrumentation package %s", importPath)
	}

	ruleFiles, walkErr := rulesFromDir(pkgDir, true)
	if walkErr != nil {
		return nil, ex.Wrapf(walkErr, "walking instrumentation package %s", importPath)
	}

	if toolFile == "" && len(ruleFiles) == 0 {
		return nil, ex.Wrapf(
			ErrNotInstrumentation,
			"instrumentation package %s contains neither %s nor any rule files",
			importPath,
			ToolFileCanonical,
		)
	}

	return &InstrumentationConfig{
		ImportPath: importPath,
		ToolFile:   toolFile,
		RuleFiles:  ruleFiles,
	}, nil
}

type InstrumentationVisit struct {
	Config *InstrumentationConfig
	Error  error
}

type InstrumentationVisitor func(visit *InstrumentationVisit) (recurse bool, err error)

func walkInstrumentation(ctx context.Context, toolFiles []string, visit InstrumentationVisitor) error {
	queue := append([]string(nil), toolFiles...)
	seenImports := make(map[string]bool)
	seenToolFiles := make(map[string]bool)

	p := ast.NewAstParser()
	for len(queue) > 0 {
		toolFile := queue[0]
		queue = queue[1:]

		f, parseErr := p.Parse(toolFile, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}

		for _, imp := range f.Imports {
			if imp.Name == nil || imp.Name.Name != ast.IdentIgnore {
				continue
			}

			importPath, unquoteErr := strconv.Unquote(imp.Path.Value)
			if unquoteErr != nil {
				return ex.Wrapf(unquoteErr, "failed to unquote import path %s in %s", imp.Path.Value, toolFile)
			}
			// OtelcToolCmdRoot is the tool import itself, not an instrumentation package.
			if importPath == util.OtelcToolCmdRoot || seenImports[importPath] {
				continue
			}
			seenImports[importPath] = true

			cfg, resolveErr := resolveInstrumentationConfig(ctx, filepath.Dir(toolFile), importPath)
			v := &InstrumentationVisit{
				Config: cfg,
				Error:  resolveErr,
			}

			recurse, visitErr := visit(v)
			if visitErr != nil {
				return visitErr
			}

			if recurse && cfg != nil && cfg.ToolFile != "" {
				// Two different import paths may share the same tool file, so we need to de-duplicate it.
				if seenToolFiles[cfg.ToolFile] {
					continue
				}
				seenToolFiles[cfg.ToolFile] = true
				queue = append(queue, cfg.ToolFile)
			}
		}
	}

	return nil
}
