// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package pkgload provides utilities for loading Go packages using the go/packages API.
package pkgload

import (
	"context"
	"path/filepath"

	"golang.org/x/tools/go/packages"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

const CommandLineArgumentsPackage = "command-line-arguments"

// LoadPackages wraps packages.Load with context and build flags.
func LoadPackages(
	ctx context.Context,
	mode packages.LoadMode,
	buildFlags []string,
	patterns ...string,
) ([]*packages.Package, error) {
	cfg := &packages.Config{
		Mode:       mode,
		Context:    ctx,
		BuildFlags: buildFlags,
	}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, ex.Wrapf(err, "loading packages %v", patterns)
	}
	return pkgs, nil
}

// ResolvePackageName returns the declared package name for an import path.
// Panics via ex.Fatalf on failure (matches existing behavior during toolexec).
func ResolvePackageName(ctx context.Context, importPath string, buildFlags ...string) string {
	pkgs, err := LoadPackages(ctx, packages.NeedName, buildFlags, importPath)
	if err != nil {
		ex.Fatalf("failed to resolve package name for %s: %v", importPath, err)
	}

	if len(pkgs) == 0 {
		ex.Fatalf("no packages found for %s", importPath)
	}

	pkg := pkgs[0]
	if len(pkg.Errors) > 0 {
		ex.Fatalf("failed to resolve package name for %s: %v", importPath, pkg.Errors[0])
	}

	if pkg.Name == "" {
		ex.Fatalf("empty package name for %s", importPath)
	}

	return pkg.Name
}

// ResolveExportFiles returns importPath -> exportFile for a package and all
// transitive dependencies.
func ResolveExportFiles(ctx context.Context, importPath string, buildFlags ...string) (map[string]string, error) {
	mode := packages.NeedName | packages.NeedImports | packages.NeedDeps | packages.NeedExportFile
	pkgs, err := LoadPackages(ctx, mode, buildFlags, importPath)
	if err != nil {
		return nil, err
	}

	if len(pkgs) == 0 {
		return nil, ex.Newf("no packages found for %q", importPath)
	}

	// Check for package-level errors
	for _, pkg := range pkgs {
		if len(pkg.Errors) > 0 {
			return nil, ex.Newf("loading package %q: %v", importPath, pkg.Errors[0])
		}
	}

	result := make(map[string]string)
	visited := make(map[string]bool)

	var walk func(pkg *packages.Package)
	walk = func(pkg *packages.Package) {
		if visited[pkg.PkgPath] {
			return
		}
		visited[pkg.PkgPath] = true

		if pkg.ExportFile != "" {
			result[pkg.PkgPath] = pkg.ExportFile
		}

		for _, dep := range pkg.Imports {
			walk(dep)
		}
	}

	for _, pkg := range pkgs {
		walk(pkg)
	}

	// Verify we found the requested package
	if _, found := result[importPath]; !found {
		return nil, ex.Newf("package %q not found or has no export file", importPath)
	}

	return result, nil
}

func GetPackageDir(pkg *packages.Package) string {
	if len(pkg.GoFiles) > 0 {
		return filepath.Dir(pkg.GoFiles[0])
	}
	return ""
}

// resolveModuleDir returns the module directory for a given package directory.
func resolveModuleDir(ctx context.Context, pkgDir string) (string, error) {
	pkgs, err := LoadPackages(ctx, packages.NeedModule, nil, pkgDir)
	if err != nil {
		return "", err
	}
	if len(pkgs) == 0 {
		return "", ex.Newf("no packages found for directory: %s", pkgDir)
	}

	pkg := pkgs[0]
	if pkg.Module == nil || pkg.Module.Dir == "" || len(pkg.Errors) > 0 {
		return "", ex.Newf(
			"failed to load module information for package in directory %s: module=%v, errors=%v",
			pkgDir,
			pkg.Module,
			pkg.Errors,
		)
	}

	return pkg.Module.Dir, nil
}

func FindModuleDirs(ctx context.Context, pkgs []*packages.Package) (map[string]bool, error) {
	logger := util.LoggerFromContext(ctx)

	moduleDirs := make(map[string]bool)
	for _, pkg := range pkgs {
		// file-based builds use synthetic "command-line-arguments" packages
		if pkg.Module == nil && pkg.PkgPath != CommandLineArgumentsPackage {
			logger.WarnContext(ctx, "skipping package without module", "package", pkg.PkgPath)
			continue
		}

		var moduleDir string
		if pkg.Module != nil {
			moduleDir = pkg.Module.Dir
		} else {
			pkgDir := GetPackageDir(pkg)
			if pkgDir == "" {
				logger.WarnContext(ctx, "skipping package without Go files", "package", pkg.PkgPath)
				continue
			}

			modDir, err := resolveModuleDir(ctx, pkgDir)
			if err != nil {
				return nil, ex.Wrapf(err, "finding module dir for package %s", pkg.PkgPath)
			}

			moduleDir = modDir
		}

		moduleDirs[moduleDir] = true
	}

	return moduleDirs, nil
}
