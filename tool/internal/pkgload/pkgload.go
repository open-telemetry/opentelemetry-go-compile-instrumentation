// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package pkgload provides utilities for loading Go packages using the go/packages API.
package pkgload

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
)

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

// ModuleDir returns the directory of dir's main module via `go env GOMOD`.
// Unlike ResolveModuleDir it doesn't load packages, so it works when the vendor
// tree is temporarily inconsistent. Returns "" outside a module.
func ModuleDir(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "go", "env", "GOMOD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", ex.Wrapf(err, "running go env GOMOD in %s", dir)
	}
	goMod := strings.TrimSpace(string(out))
	// go env GOMOD prints os.DevNull when dir is not inside a module.
	if goMod == "" || goMod == os.DevNull {
		return "", nil
	}
	return filepath.Dir(goMod), nil
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

// ResolveModuleDir returns the module directory for a given package directory.
func ResolveModuleDir(ctx context.Context, pkgDir string) (string, error) {
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
