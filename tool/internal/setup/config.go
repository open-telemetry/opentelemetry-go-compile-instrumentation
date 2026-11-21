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

	"golang.org/x/tools/go/packages"

	"github.com/dave/dst"
	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/pkgload"
	"go.opentelemetry.io/otelc/tool/internal/session"
	"go.opentelemetry.io/otelc/tool/util"
)

const (
	// Allowed names for the instrumentation config file.
	toolFileCanonical            = "otel.instrumentation.go"
	toolFileAlias                = "otelc.tool.go"
	instrumentationYAMLCanonical = "otel.instrumentation.yml"
	instrumentationYAMLAlias     = "otel.instrumentation.yaml"
)

type instrumentationConfig struct {
	ImportPath string
	ModuleDir  string
	ToolFile   string
	RuleFiles  []string
	Error      error
}

func findOneOf(moduleDir, canonicalName, aliasName, label string) (string, error) {
	canonical := filepath.Join(moduleDir, canonicalName)
	alias := filepath.Join(moduleDir, aliasName)

	switch canonicalExists, aliasExists := util.PathExists(canonical), util.PathExists(alias); {
	case canonicalExists && aliasExists:
		return "", ex.Newf(
			"both %q and %q exist; only one %s file is allowed",
			canonicalName,
			aliasName,
			label,
		)
	case canonicalExists:
		return canonical, nil
	case aliasExists:
		return alias, nil
	default:
		return "", nil
	}
}

func findInstrumentationYAMLFile(moduleDir string) (string, error) {
	return findOneOf(
		moduleDir,
		instrumentationYAMLCanonical,
		instrumentationYAMLAlias,
		"instrumentation YAML",
	)
}

//nolint:forbidigo // sentinel error; must not carry mutable stack state
var errNotInstrumentation = errors.New("not an instrumentation package")

func findToolFile(moduleDir string) (string, error) {
	return findOneOf(moduleDir, toolFileCanonical, toolFileAlias, "instrumentation config")
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

// resolveInstrumentationConfigs resolves instrumentation configs for the given import paths.
//
//nolint:nilnil // nil, nil when no imports are specified
func resolveInstrumentationConfigs(
	ctx context.Context,
	dir string,
	importPaths []string,
) (map[string]*instrumentationConfig, error) {
	if len(importPaths) == 0 {
		return nil, nil
	}

	var pkgs []*packages.Package
	loadErr := session.WithLoadSlot(ctx, func(ctx context.Context) error {
		var err error
		pkgs, err = pkgload.LoadPackages(ctx,
			packages.NeedFiles|packages.NeedModule|packages.NeedName,
			[]string{"-C", dir}, false, importPaths...)
		return err
	})
	if loadErr != nil {
		return nil, ex.Wrapf(loadErr, "failed to load instrumentation packages")
	}

	cfgs := make(map[string]*instrumentationConfig, len(importPaths))
	for _, pkg := range pkgs {
		importPath := pkg.PkgPath
		if importPath == "" {
			continue
		}

		cfgs[importPath] = &instrumentationConfig{
			ImportPath: importPath,
		}

		if pkg.Module == nil || pkg.Module.Dir == "" {
			cfgs[importPath].Error = ex.Newf("package %s is not part of a module", importPath)
			continue
		}

		modDir := pkg.Module.Dir
		cfgs[importPath].ModuleDir = modDir
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
				errNotInstrumentation,
				"instrumentation package %s contains neither %s nor any rule files",
				importPath,
				toolFileCanonical,
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

type instrumentationVisit struct {
	Config   *instrumentationConfig
	ToolFile string
}

type instrumentationVisitor func(visit *instrumentationVisit) (recurse bool, err error)

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
func walkInstrumentation(ctx context.Context, toolFiles []string, visit instrumentationVisitor) error {
	p := ast.NewAstParser()
	seenRoots := make(map[string]bool, len(toolFiles))
	for _, rootToolFile := range toolFiles {
		if seenRoots[rootToolFile] {
			continue
		}
		seenRoots[rootToolFile] = true

		// Keep import and nested-tool-file de-duplication local to each root.
		// Two YAML files may select the same instrumentation independently;
		// each owning root must still receive its own invalid-import result so
		// pruning one selection cannot leave the other stale.
		if err := walkInstrumentationRoot(ctx, p, rootToolFile, visit); err != nil {
			return err
		}
	}

	return nil
}

func walkInstrumentationRoot(
	ctx context.Context,
	p *ast.AstParser,
	rootToolFile string,
	visit instrumentationVisitor,
) error {
	queue := []string{rootToolFile}
	seenImports := make(map[string]bool)
	seenToolFiles := map[string]bool{rootToolFile: true}
	for len(queue) > 0 {
		toolFile := queue[0]
		queue = queue[1:]

		f, err := p.Parse(toolFile, parser.ImportsOnly)
		if err != nil {
			return err
		}
		importPaths, err := collectImports(toolFile, f, seenImports)
		if err != nil {
			return err
		}
		cfgs, err := resolveInstrumentationConfigs(ctx, filepath.Dir(toolFile), importPaths)
		if err != nil {
			return err
		}
		for _, importPath := range importPaths {
			cfg := cfgs[importPath]
			if cfg == nil {
				continue
			}
			nestedToolFile, visitErr := visitInstrumentationImport(visit, cfg, toolFile, seenToolFiles)
			if visitErr != nil {
				return visitErr
			}
			if nestedToolFile != "" {
				queue = append(queue, nestedToolFile)
			}
		}
	}
	return nil
}

func visitInstrumentationImport(
	visit instrumentationVisitor,
	cfg *instrumentationConfig,
	toolFile string,
	seenToolFiles map[string]bool,
) (string, error) {
	v := &instrumentationVisit{Config: cfg, ToolFile: toolFile}
	recurse, err := visit(v)
	if err != nil || !recurse || cfg.ToolFile == "" || seenToolFiles[cfg.ToolFile] {
		return "", err
	}
	seenToolFiles[cfg.ToolFile] = true
	return cfg.ToolFile, nil
}
