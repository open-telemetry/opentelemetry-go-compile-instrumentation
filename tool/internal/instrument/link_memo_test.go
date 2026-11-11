// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/imports"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

// countingResolver is a fake nested go list that resolves every root to a
// closure of archive files it creates under dir (the root plus deps), except
// the roots in bad, and counts the calls and the roots each call lists.
type countingResolver struct {
	dir   string
	bad   map[string]bool
	deps  []string
	calls int
	roots [][]string
}

func (c *countingResolver) resolve(
	_ context.Context, roots []string, _ ...string,
) (map[string]map[string]string, map[string]error, error) {
	c.calls++
	c.roots = append(c.roots, roots)
	closures := make(map[string]map[string]string)
	failed := make(map[string]error)
	for _, root := range roots {
		if c.bad[root] {
			failed[root] = errors.New("cannot load " + root)
			continue
		}
		archive := filepath.Join(c.dir, util.EscapePackagePath(root)+".a")
		if err := os.WriteFile(archive, []byte(root), 0o644); err != nil {
			return nil, nil, err
		}
		closures[root] = map[string]string{root: archive}
		for _, dep := range c.deps {
			closures[root][dep] = filepath.Join(c.dir, util.EscapePackagePath(dep)+".a")
			if err := os.WriteFile(closures[root][dep], []byte(dep), 0o644); err != nil {
				return nil, nil, err
			}
		}
	}
	return closures, failed, nil
}

// link runs one link's recovery with the counting resolver.
func (c *countingResolver) link(t *testing.T, cfg *imports.ImportConfig) bool {
	t.Helper()
	updated, err := resolveRuleAddedImportsWith(t.Context(), nil, cfg, c.resolve)
	require.NoError(t, err)
	return updated
}

// setupLinkMemo prepares a work dir with a session, a resolution scope, and
// matched rules that add imports of the given paths to example.com/app/dep.
func setupLinkMemo(t *testing.T, adds ...string) {
	t.Helper()
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	t.Setenv(util.EnvOtelcSessionDir, filepath.Join(t.TempDir(), "session"))
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
	require.NoError(t, os.WriteFile(util.GetResolutionScopeFile(), []byte("scope"), 0o644))
	importsMap := make(map[string]string, len(adds))
	for i, path := range adds {
		importsMap["i"+string(rune('a'+i))] = path
	}
	writeMatchedRules(t, []*rule.InstRuleSet{{
		PackageName: "dep",
		ModulePath:  "example.com/app/dep",
		FuncRules: map[string][]*rule.InstFuncRule{
			"dep.go": {{InstBaseRule: rule.InstBaseRule{Name: "rule", Imports: importsMap}}},
		},
	}})
}

func freshLinkConfig() *imports.ImportConfig {
	return &imports.ImportConfig{PackageFile: map[string]string{"example.com/app/dep": "/unused/dep.a"}}
}

func TestLinkResolutionReusesMemoAcrossLinks(t *testing.T) {
	setupLinkMemo(t, "example.com/extra")
	resolver := &countingResolver{dir: t.TempDir()}

	first := freshLinkConfig()
	updated := resolver.link(t, first)
	assert.True(t, updated)
	second := freshLinkConfig()
	updated = resolver.link(t, second)
	assert.True(t, updated)

	assert.Equal(t, 1, resolver.calls, "the second link must read the memo instead of running go list")
	assert.Equal(t, first.PackageFile, second.PackageFile)
	assert.Contains(t, second.PackageFile, "example.com/extra")
}

func TestLinkResolutionReResolvesStaleMemoEntry(t *testing.T) {
	setupLinkMemo(t, "example.com/extra")
	resolver := &countingResolver{dir: t.TempDir()}

	cfg := freshLinkConfig()
	resolver.link(t, cfg)
	require.NoError(t, os.Remove(cfg.PackageFile["example.com/extra"]))

	resolver.link(t, freshLinkConfig())
	assert.Equal(t, 2, resolver.calls, "an entry whose archive is gone must be resolved again")
}

// TestLinkResolutionReResolvesIncompatibleArchive covers a memo entry
// resolved against other deps: its closure maps a package the link already
// lists to another archive, so the link must resolve it again rather than
// mix the two builds.
func TestLinkResolutionReResolvesIncompatibleArchive(t *testing.T) {
	setupLinkMemo(t, "example.com/extra")
	resolver := &countingResolver{dir: t.TempDir(), deps: []string{"example.com/shared"}}

	resolver.link(t, freshLinkConfig())
	require.Equal(t, 1, resolver.calls)

	// The link compiled example.com/shared itself, in its own $WORK.
	cfg := freshLinkConfig()
	own := filepath.Join(t.TempDir(), "b007", "_pkg_.a")
	cfg.PackageFile["example.com/shared"] = own
	resolver.link(t, cfg)
	assert.Equal(t, 2, resolver.calls, "an entry built against another archive must be resolved again")
	assert.Equal(t, own, cfg.PackageFile["example.com/shared"], "the link keeps its own archive")
}

func TestLinkResolutionListsFailedCandidateOnce(t *testing.T) {
	setupLinkMemo(t, "example.com/extra", "example.invalid/bad")
	resolver := &countingResolver{dir: t.TempDir(), bad: map[string]bool{"example.invalid/bad": true}}

	cfg := freshLinkConfig()
	resolver.link(t, cfg)
	require.Equal(t, 1, resolver.calls)
	assert.NotContains(t, cfg.PackageFile, "example.invalid/bad")

	// A later link reads the failure from the memo too.
	resolver.link(t, freshLinkConfig())
	assert.Equal(t, 1, resolver.calls)
}

func TestLinkResolutionSkipsGoListWhenNothingIsMissing(t *testing.T) {
	setupLinkMemo(t, "example.com/extra")
	resolver := &countingResolver{dir: t.TempDir()}

	cfg := freshLinkConfig()
	cfg.PackageFile["example.com/extra"] = "/unused/extra.a"
	updated := resolver.link(t, cfg)
	assert.False(t, updated)
	assert.Zero(t, resolver.calls)
}

func TestLinkResolutionWithoutSessionResolvesEveryLink(t *testing.T) {
	setupLinkMemo(t, "example.com/extra")
	t.Setenv(util.EnvOtelcSessionDir, util.SessionOff)
	resolver := &countingResolver{dir: t.TempDir()}

	for range 2 {
		resolver.link(t, freshLinkConfig())
	}
	assert.Equal(t, 2, resolver.calls)
}

func TestCompileResolutionReusesMemo(t *testing.T) {
	setupLinkMemo(t)
	archiveDir := t.TempDir()
	calls := 0
	resolve := func(_ context.Context, importPath string, _ ...string) (map[string]string, error) {
		calls++
		archive := filepath.Join(archiveDir, "extra.a")
		if err := os.WriteFile(archive, []byte(importPath), 0o644); err != nil {
			return nil, err
		}
		return map[string]string{importPath: archive}, nil
	}

	compile := func() string {
		cfgPath := filepath.Join(t.TempDir(), "importcfg")
		require.NoError(t, os.WriteFile(cfgPath, []byte("packagefile context=/unused/context.a\n"), 0o644))
		ip := &instrumentPhase{
			logger:             slog.Default(),
			importConfigPath:   cfgPath,
			importConfig:       imports.ImportConfig{PackageFile: map[string]string{"context": "/unused/context.a"}},
			resolveAddedImport: resolve,
		}
		require.NoError(t, ip.updateImportConfig(t.Context(), map[string]string{"extra": "example.com/extra"}))
		content, err := os.ReadFile(cfgPath)
		require.NoError(t, err)
		return string(content)
	}

	first := compile()
	second := compile()
	assert.Equal(t, 1, calls, "the second compile must read the memo instead of running go list")
	assert.Equal(t, first, second)
	assert.Contains(t, second, "packagefile example.com/extra=")
}
