// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"

	"go.opentelemetry.io/otelc/tool/internal/rule"
)

// countingGraphPhase returns a setup phase over a module in dir whose graph
// loads are counted per requested path.
func countingGraphPhase(dir string, loaded map[string]int) *setupPhase {
	sp := &setupPhase{logger: slog.New(slog.DiscardHandler), buildFlags: []string{"-C", dir}}
	sp.graph.load = func(ctx context.Context, flags, paths []string) ([]*packages.Package, error) {
		for _, p := range paths {
			loaded[p]++
		}
		return loadGraphPackages(ctx, flags, paths)
	}
	return sp
}

func hookRuleSets(path string) []*rule.InstRuleSet {
	return []*rule.InstRuleSet{{FileRules: []*rule.InstFileRule{{Path: path}}}}
}

func TestPackageGraphSharedAcrossSetupLoads(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "go.mod"), "module example.com/graphtest\n\ngo 1.24\n")
	mustWriteFile(t, filepath.Join(dir, "hooks", "hooks.go"),
		"package hooks\n\nimport _ \"example.com/graphtest/internal\"\n")
	mustWriteFile(t, filepath.Join(dir, "internal", "internal.go"), "package internal\n")
	const hook = "example.com/graphtest/hooks"

	loaded := map[string]int{}
	sp := countingGraphPhase(dir, loaded)

	deps, err := injectedDepsFrom(t.Context(), []string{hook}, map[string]bool{}, sp.graphLoader(sp.buildFlags))
	require.NoError(t, err)
	assert.Len(t, deps, 2)

	imports, err := sp.hookImports(t.Context(), hookRuleSets(hook))
	require.NoError(t, err)
	assert.True(t, imports[hook]["example.com/graphtest/internal"])

	scopeDeps, err := sp.addedPackageDeps(t.Context(), map[string]bool{hook: true}, nil)
	require.NoError(t, err)
	assert.True(t, scopeDeps[hook]["example.com/graphtest/internal"])

	assert.Equal(t, map[string]int{hook: 1}, loaded,
		"the hook imports and the runtime scope must reuse the injected-deps load")
}

func TestPackageGraphKeepsPerCallerErrorHandling(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "go.mod"), "module example.com/graphtest\n\ngo 1.24\n")
	mustWriteFile(t, filepath.Join(dir, "broken", "broken.go"),
		"package broken\n\nimport _ \"example.com/graphtest/missing\"\n")
	const hook = "example.com/graphtest/broken"

	loaded := map[string]int{}
	sp := countingGraphPhase(dir, loaded)

	// The injected-deps pass degrades to a warning on a broken closure.
	extra, err := sp.matchInjectedDeps(t.Context(), hookRuleSets(hook), nil, nil, sp.buildFlags)
	require.NoError(t, err)
	assert.Empty(t, extra)

	// The hook imports still fail on the cached package's error.
	_, err = sp.hookImports(t.Context(), hookRuleSets(hook))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "example.com/graphtest/missing")
	assert.Equal(t, 1, loaded[hook])
}

func TestPackageGraphResetsOnOtherFlags(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "go.mod"), "module example.com/graphtest\n\ngo 1.24\n")
	mustWriteFile(t, filepath.Join(dir, "hooks", "hooks.go"), "package hooks\n")
	const hook = "example.com/graphtest/hooks"

	loaded := map[string]int{}
	sp := countingGraphPhase(dir, loaded)
	_, err := sp.loadGraph(t.Context(), []string{"-C", dir}, []string{hook})
	require.NoError(t, err)
	_, err = sp.loadGraph(t.Context(), []string{"-C", dir, "-tags=x"}, []string{hook})
	require.NoError(t, err)
	assert.Equal(t, 2, loaded[hook], "a load with other build flags must not reuse the cache")
}

// TestPackageGraphCachesPathsLoadedUnderAnotherImportPath covers a requested
// path that is not an import path, such as a relative directory: its root
// loads under the import path, and the cache must still answer the
// requested path next time instead of loading it again.
func TestPackageGraphCachesPathsLoadedUnderAnotherImportPath(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "go.mod"), "module example.com/graphtest\n\ngo 1.24\n")
	mustWriteFile(t, filepath.Join(dir, "hooks", "hooks.go"), "package hooks\n")

	loaded := map[string]int{}
	sp := countingGraphPhase(dir, loaded)
	for range 2 {
		pkgs, err := sp.loadGraph(t.Context(), sp.buildFlags, []string{"./hooks"})
		require.NoError(t, err)
		require.Len(t, pkgs, 1)
		assert.Equal(t, "example.com/graphtest/hooks", pkgs[0].PkgPath)
	}
	assert.Equal(t, 1, loaded["./hooks"], "a path loaded under another import path must hit the cache")
}
