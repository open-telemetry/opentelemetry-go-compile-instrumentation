// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"errors"
	"go/parser"
	"path/filepath"
	"strconv"
	"strings"

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
	Dir        string
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
		return "", ex.Wrapf(
			ErrNotInstrumentation,
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

func resolveInstrumentationConfig(ctx context.Context, moduleDir, importPath string) (*InstrumentationConfig, error) {
	goModPath := filepath.Join(moduleDir, "go.mod")
	modFile, modErr := parseGoMod(goModPath)
	if modErr != nil {
		return nil, ex.Wrapf(modErr, "preparing go.mod for instrumentation package %s", importPath)
	}

	isLocal := false
	if modFile.Module != nil {
		modulePath := modFile.Module.Mod.Path
		isLocal = importPath == modulePath || strings.HasPrefix(importPath, modulePath+"/")
	}

	if !isLocal {
		required, reqErr := addRequire(modFile, importPath)
		if reqErr != nil {
			return nil, ex.Wrapf(reqErr, "ensuring instrumentation package %s is required", importPath)
		}
		if required {
			if writeErr := writeGoMod(goModPath, modFile); writeErr != nil {
				return nil, ex.Wrapf(writeErr, "writing updated go.mod for instrumentation package %s", importPath)
			}
		}
	}

	pkgs, loadErr := packages.Load(&packages.Config{
		Mode:       packages.NeedFiles,
		Context:    ctx,
		Dir:        moduleDir,
		BuildFlags: []string{"-mod=mod"},
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

	pkgDir := pkgload.GetPackageDir(pkg)
	if pkgDir == "" {
		return nil, ex.Newf("could not determine directory for package %s", importPath)
	}

	// Instrumentation packages must be module roots.
	goModPath = filepath.Join(pkgDir, "go.mod")
	if !util.PathExists(goModPath) {
		return nil, ex.Wrapf(ErrNotInstrumentation, "instrumentation package %s does not contain a go.mod", importPath)
	}

	cfg := &InstrumentationConfig{
		ImportPath: importPath,
		Dir:        pkgDir,
	}

	toolFile, findErr := findToolFile(pkgDir)
	if findErr != nil {
		return nil, ex.Wrapf(findErr, "checking for tool file in instrumentation package %s", importPath)
	}
	cfg.ToolFile = toolFile

	ruleFiles, walkErr := rulesFromDir(pkgDir, true)
	if walkErr != nil {
		return nil, ex.Wrapf(walkErr, "walking instrumentation package %s", importPath)
	}
	cfg.RuleFiles = ruleFiles

	if cfg.ToolFile == "" && len(cfg.RuleFiles) == 0 {
		return nil, ex.Wrapf(
			ErrNotInstrumentation,
			"instrumentation package %s contains neither %s nor any rule files",
			importPath,
			ToolFileCanonical,
		)
	}

	return cfg, nil
}

type InstrumentationVisit struct {
	ToolFile   string
	ImportPath string
	Config     *InstrumentationConfig
	Error      error
}

type InstrumentationVisitor func(visit *InstrumentationVisit) (recurse bool, err error)

func walkInstrumentation(ctx context.Context, toolFiles []string, visit InstrumentationVisitor) error {
	queue := append([]string(nil), toolFiles...)
	seenImports := make(map[string]bool)

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

			importPath, unqoteErr := strconv.Unquote(imp.Path.Value)
			if unqoteErr != nil {
				return ex.Wrapf(unqoteErr, "failed to unquote import path %s in %s", imp.Path.Value, toolFile)
			}

			// OtelcToolCmdRoot is the tool import itself, not an instrumentation package.
			if importPath == util.OtelcToolCmdRoot || seenImports[importPath] {
				continue
			}
			seenImports[importPath] = true

			cfg, resolveErr := resolveInstrumentationConfig(ctx, filepath.Dir(toolFile), importPath)
			v := &InstrumentationVisit{
				ToolFile:   toolFile,
				ImportPath: importPath,
				Config:     cfg,
				Error:      resolveErr,
			}

			recurse, visitErr := visit(v)
			if visitErr != nil {
				return visitErr
			}

			if recurse && cfg != nil && cfg.ToolFile != "" {
				queue = append(queue, cfg.ToolFile)
			}
		}
	}

	return nil
}
