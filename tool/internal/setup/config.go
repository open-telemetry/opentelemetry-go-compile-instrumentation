// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"errors"
	"go/parser"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/tools/go/packages"

	"github.com/dave/dst"
	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/pkgload"
	"go.opentelemetry.io/otelc/tool/util"
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
	Error      error
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
	// Sort for deterministic rule loading.
	slices.Sort(toolFiles)
	return toolFiles, nil
}

const packagesLoadTimeout = 30 * time.Second

// resolveInstrumentationConfigs resolves instrumentation configs for the given import paths.
//
//nolint:nilnil // nil, nil when no imports are specified
func resolveInstrumentationConfigs(
	ctx context.Context,
	dir string,
	importPaths []string,
) (map[string]*InstrumentationConfig, error) {
	if len(importPaths) == 0 {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(ctx, packagesLoadTimeout)
	defer cancel()

	pkgs, loadErr := packages.Load(&packages.Config{
		Mode:    packages.NeedFiles | packages.NeedModule | packages.NeedName,
		Context: ctx,
		Dir:     dir,
	}, importPaths...)
	if loadErr != nil {
		return nil, ex.Wrapf(loadErr, "failed to load instrumentation packages")
	}

	cfgs := make(map[string]*InstrumentationConfig, len(importPaths))
	for _, pkg := range pkgs {
		importPath := pkg.PkgPath
		if importPath == "" {
			continue
		}

		cfgs[importPath] = &InstrumentationConfig{
			ImportPath: importPath,
		}

		if pkg.Module == nil || pkg.Module.Dir == "" {
			cfgs[importPath].Error = ex.Newf("package %s is not part of a module", importPath)
			continue
		}

		modDir := pkg.Module.Dir
		// Prefer the directory of a real Go file. A tools-only instrumentation
		// package (its only .go file carries //go:build tools) has no buildable Go
		// files, so packages.Load reports "build constraints exclude all Go files"
		// and GoFiles is empty; fall back to deriving the dir from the module path +
		// import-path suffix (correct even under replace directives). The walk only
		// needs the directory, not a compilable package.
		pkgDir := pkgload.PackageDir(pkg)
		if pkgDir == "" {
			pkgDir = filepath.Join(modDir, filepath.FromSlash(strings.TrimPrefix(importPath, pkg.Module.Path)))
		}

		// Always look for tool file in the module directory
		toolFile, findErr := findToolFile(modDir)
		if findErr != nil {
			cfgs[importPath].Error = ex.Wrapf(
				findErr,
				"checking for tool file in instrumentation package %s",
				importPath,
			)
			continue
		}

		ruleFiles, walkErr := rulesFromDir(pkgDir, true)
		if walkErr != nil {
			cfgs[importPath].Error = ex.Wrapf(walkErr, "walking instrumentation package %s", importPath)
			continue
		}

		if toolFile == "" && len(ruleFiles) == 0 {
			err := ex.Wrapf(
				ErrNotInstrumentation,
				"instrumentation package %s contains neither %s nor any rule files",
				importPath,
				ToolFileCanonical,
			)

			if len(pkg.Errors) > 0 {
				var msgs []string
				for _, e := range pkg.Errors {
					msgs = append(msgs, e.Error())
				}
				err = ex.Wrapf(err, "package load errors: %s", strings.Join(msgs, "; "))
			}

			cfgs[importPath].Error = err
			continue
		}

		cfgs[importPath].ToolFile = toolFile
		cfgs[importPath].RuleFiles = ruleFiles
	}

	return cfgs, nil
}

type InstrumentationVisit struct {
	Config   *InstrumentationConfig
	ToolFile string
}

type InstrumentationVisitor func(visit *InstrumentationVisit) (recurse bool, err error)

func collectImports(toolFile string, f *dst.File, seenImports map[string]bool) ([]string, error) {
	importPaths := make([]string, 0, len(f.Imports))

	for _, imp := range f.Imports {
		if imp.Name == nil {
			return nil, ex.Newf(
				"%s: import %s must be a blank import (use `_ %s`)",
				toolFile,
				imp.Path.Value,
				imp.Path.Value,
			)
		}
		if imp.Name.Name != ast.IdentIgnore {
			return nil, ex.Newf(
				"%s: import %s must be a blank import (named imports are not allowed)",
				toolFile,
				imp.Path.Value,
			)
		}

		importPath, unquoteErr := strconv.Unquote(imp.Path.Value)
		if unquoteErr != nil {
			return nil, ex.Wrapf(
				unquoteErr,
				"failed to unquote import path %s in %s",
				imp.Path.Value,
				toolFile,
			)
		}

		if seenImports[importPath] {
			continue
		}

		seenImports[importPath] = true
		importPaths = append(importPaths, importPath)
	}

	return importPaths, nil
}

// walkInstrumentation walks the instrumentation tool files and calls the visitor function for each.
func walkInstrumentation(ctx context.Context, toolFiles []string, visit InstrumentationVisitor) error {
	queue := append([]string(nil), toolFiles...)
	seenImports := make(map[string]bool)
	seenToolFiles := make(map[string]bool, len(toolFiles))
	for _, toolFile := range toolFiles {
		seenToolFiles[toolFile] = true
	}

	p := ast.NewAstParser()
	for len(queue) > 0 {
		toolFile := queue[0]
		queue = queue[1:]

		f, parseErr := p.Parse(toolFile, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}

		importPaths, collectErr := collectImports(toolFile, f, seenImports)
		if collectErr != nil {
			return collectErr
		}

		cfgs, resolveErr := resolveInstrumentationConfigs(ctx, filepath.Dir(toolFile), importPaths)
		if resolveErr != nil {
			return resolveErr
		}

		for _, importPath := range importPaths {
			cfg := cfgs[importPath]
			if cfg == nil {
				continue
			}

			v := &InstrumentationVisit{
				Config:   cfg,
				ToolFile: toolFile,
			}
			recurse, visitErr := visit(v)
			if visitErr != nil {
				return visitErr
			}

			if recurse && cfg.ToolFile != "" {
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
