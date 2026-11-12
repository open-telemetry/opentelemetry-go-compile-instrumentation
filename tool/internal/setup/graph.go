// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"slices"

	"golang.org/x/tools/go/packages"

	"go.opentelemetry.io/otelc/tool/internal/pkgload"
	"go.opentelemetry.io/otelc/tool/internal/session"
)

// graphLoadMode is the load mode of the package graph cache: the superset of
// what the injected-dependency, hook-import, and runtime-scope loads need.
const graphLoadMode = packages.NeedName | packages.NeedFiles | packages.NeedImports | packages.NeedDeps

// graphLoader loads the package graph rooted at paths.
type graphLoader func(ctx context.Context, paths []string) ([]*packages.Package, error)

// packageGraph caches the roots setup's package graph loads returned, so the
// injected-dependency passes, the hook imports, and the runtime scope load
// every root once. A cached package keeps its own load errors; each caller
// decides what they mean. A failed load caches nothing.
type packageGraph struct {
	flags []string
	// roots maps a requested path to the roots its load returned: normally
	// the one package with that import path, but a path that is not an
	// import path, such as a relative directory, loads under another one.
	roots map[string][]*packages.Package
	// load replaces the package load in tests.
	load func(ctx context.Context, flags, paths []string) ([]*packages.Package, error)
}

// loadGraphPackages loads paths under a session load slot.
func loadGraphPackages(ctx context.Context, flags, paths []string) ([]*packages.Package, error) {
	var pkgs []*packages.Package
	err := session.WithLoadSlot(ctx, func(ctx context.Context) error {
		var loadErr error
		pkgs, loadErr = pkgload.LoadPackages(ctx, graphLoadMode, flags, false, paths...)
		return loadErr
	})
	return pkgs, err
}

// graphLoader returns the loader of the graph cache for the given build
// flags. The cache holds one set of flags; other flags start it afresh.
func (sp *setupPhase) graphLoader(flags []string) graphLoader {
	return func(ctx context.Context, paths []string) ([]*packages.Package, error) {
		return sp.loadGraph(ctx, flags, paths)
	}
}

// loadGraph returns the graph roots for paths, loading only the paths no
// earlier call loaded with the same flags.
func (sp *setupPhase) loadGraph(ctx context.Context, flags, paths []string) ([]*packages.Package, error) {
	g := &sp.graph
	if g.roots == nil || !slices.Equal(g.flags, flags) {
		g.flags = slices.Clone(flags)
		g.roots = make(map[string][]*packages.Package)
	}
	var missing []string
	for _, p := range paths {
		if _, ok := g.roots[p]; !ok {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		load := g.load
		if load == nil {
			load = loadGraphPackages
		}
		loaded, err := load(ctx, flags, missing)
		if err != nil {
			return nil, err
		}
		g.store(missing, loaded)
	}
	pkgs := make([]*packages.Package, 0, len(paths))
	seen := make(map[*packages.Package]bool, len(paths))
	for _, p := range paths {
		for _, pkg := range g.roots[p] {
			if !seen[pkg] {
				seen[pkg] = true
				pkgs = append(pkgs, pkg)
			}
		}
	}
	return pkgs, nil
}

// store records the roots one load returned for the requested paths. A
// root is stored under its import path when that path was requested. The
// roots that match no requested path are stored under every requested path
// that matched no root, so those paths hit the cache next time instead of
// loading again.
func (g *packageGraph) store(requested []string, loaded []*packages.Package) {
	want := make(map[string]bool, len(requested))
	for _, p := range requested {
		want[p] = true
	}
	var unmatched []*packages.Package
	for _, pkg := range loaded {
		if want[pkg.PkgPath] {
			g.roots[pkg.PkgPath] = append(g.roots[pkg.PkgPath], pkg)
			continue
		}
		unmatched = append(unmatched, pkg)
	}
	for _, p := range requested {
		if _, ok := g.roots[p]; !ok {
			g.roots[p] = unmatched
		}
	}
}
