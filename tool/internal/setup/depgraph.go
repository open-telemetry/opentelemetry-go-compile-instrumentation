// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"slices"
	"sync"

	"golang.org/x/tools/go/packages"
)

// importGraph holds the imports of the packages that setup loaded. It answers
// whether a compile can use a type of a given package: Go code can only name
// or hold a value of a type when the package of the type is its own package or
// one of its dependencies.
//
// The graph is keyed by import path, so the test variants of a package and
// the package itself share their imports. A package that go/packages loaded
// with errors can import anything.
//
// add must not run at the same time as reaches. reaches is safe for
// concurrent use.
type importGraph struct {
	mu sync.Mutex
	// imports maps an import path to the import paths it imports.
	imports map[string]map[string]bool
	// byFile maps a source file to the import paths of the packages that
	// compile it.
	byFile map[string][]string
	// unknown holds the import paths of the packages loaded with errors.
	unknown map[string]bool
	// importedBy and importers are filled on demand and reset by add.
	// importedBy maps an import path to the import paths that import it, and
	// importers maps an import path to the import paths that depend on it.
	importedBy map[string][]string
	importers  map[string]map[string]bool
}

// add adds pkgs and their dependencies to the graph.
func (g *importGraph) add(pkgs []*packages.Package) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.imports == nil {
		g.imports = make(map[string]map[string]bool)
		g.byFile = make(map[string][]string)
		g.unknown = make(map[string]bool)
	}
	g.importedBy, g.importers = nil, nil
	packages.Visit(pkgs, nil, func(pkg *packages.Package) {
		if pkg.PkgPath == "" {
			return
		}
		if len(pkg.Errors) > 0 {
			g.unknown[pkg.PkgPath] = true
		}
		imports := g.imports[pkg.PkgPath]
		if imports == nil {
			imports = make(map[string]bool, len(pkg.Imports))
			g.imports[pkg.PkgPath] = imports
		}
		for _, imp := range pkg.Imports {
			imports[imp.PkgPath] = true
		}
		for _, file := range pkg.GoFiles {
			paths := g.byFile[file]
			if !slices.Contains(paths, pkg.PkgPath) {
				g.byFile[file] = append(paths, pkg.PkgPath)
			}
		}
	})
}

// reaches reports whether the compile of dep can use the types of the package
// pkgPath. It reports true when the graph does not know dep.
func (g *importGraph) reaches(dep *Dependency, pkgPath string) bool {
	if g == nil {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	compiled := g.packagesOf(dep)
	if len(compiled) == 0 {
		return true
	}
	importers := g.importersOf(pkgPath)
	for _, path := range compiled {
		if importers[path] {
			return true
		}
	}
	return false
}

// packagesOf returns the import paths of the graph's packages that dep
// compiles. Every main package compiles with -p main, so dep is found by its
// source files first.
func (g *importGraph) packagesOf(dep *Dependency) []string {
	var paths []string
	for _, src := range dep.Sources {
		for _, path := range g.byFile[src] {
			if !slices.Contains(paths, path) {
				paths = append(paths, path)
			}
		}
	}
	if _, ok := g.imports[dep.ImportPath]; ok && !slices.Contains(paths, dep.ImportPath) {
		paths = append(paths, dep.ImportPath)
	}
	return paths
}

// importersOf returns the import paths that depend on pkgPath or on a package
// loaded with errors, the packages themselves included.
func (g *importGraph) importersOf(pkgPath string) map[string]bool {
	if found, ok := g.importers[pkgPath]; ok {
		return found
	}
	if g.importedBy == nil {
		g.importedBy = make(map[string][]string, len(g.imports))
		for path, imports := range g.imports {
			for imp := range imports {
				g.importedBy[imp] = append(g.importedBy[imp], path)
			}
		}
	}
	found := map[string]bool{pkgPath: true}
	queue := []string{pkgPath}
	for path := range g.unknown {
		if !found[path] {
			found[path] = true
			queue = append(queue, path)
		}
	}
	for len(queue) > 0 {
		path := queue[0]
		queue = queue[1:]
		for _, importer := range g.importedBy[path] {
			if !found[importer] {
				found[importer] = true
				queue = append(queue, importer)
			}
		}
	}
	if g.importers == nil {
		g.importers = make(map[string]map[string]bool)
	}
	g.importers[pkgPath] = found
	return found
}
