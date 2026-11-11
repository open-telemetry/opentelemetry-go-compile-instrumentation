// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/imports"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

// countingResolver replaces resolveExportClosures with a fake that resolves
// every root to a closure of archive files it creates under dir, except the
// roots in bad, and counts the calls and the roots each call lists.
type countingResolver struct {
	dir   string
	bad   map[string]bool
	calls int
	roots [][]string
}

func (c *countingResolver) install(t *testing.T) {
	t.Helper()
	prev := resolveExportClosures
	resolveExportClosures = func(
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
		}
		return closures, failed, nil
	}
	t.Cleanup(func() { resolveExportClosures = prev })
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

func newLinkConfig() *imports.ImportConfig {
	return &imports.ImportConfig{PackageFile: map[string]string{"example.com/app/dep": "/unused/dep.a"}}
}

func TestLinkResolutionReusesMemoAcrossLinks(t *testing.T) {
	setupLinkMemo(t, "example.com/extra")
	resolver := &countingResolver{dir: t.TempDir()}
	resolver.install(t)

	first := newLinkConfig()
	updated, err := resolveRuleAddedImports(t.Context(), nil, first)
	require.NoError(t, err)
	assert.True(t, updated)
	second := newLinkConfig()
	updated, err = resolveRuleAddedImports(t.Context(), nil, second)
	require.NoError(t, err)
	assert.True(t, updated)

	assert.Equal(t, 1, resolver.calls, "the second link must read the memo instead of running go list")
	assert.Equal(t, first.PackageFile, second.PackageFile)
	assert.Contains(t, second.PackageFile, "example.com/extra")
}

func TestLinkResolutionReResolvesStaleMemoEntry(t *testing.T) {
	setupLinkMemo(t, "example.com/extra")
	resolver := &countingResolver{dir: t.TempDir()}
	resolver.install(t)

	cfg := newLinkConfig()
	_, err := resolveRuleAddedImports(t.Context(), nil, cfg)
	require.NoError(t, err)
	require.NoError(t, os.Remove(cfg.PackageFile["example.com/extra"]))

	_, err = resolveRuleAddedImports(t.Context(), nil, newLinkConfig())
	require.NoError(t, err)
	assert.Equal(t, 2, resolver.calls, "an entry whose archive is gone must be resolved again")
}

func TestLinkResolutionListsFailedCandidateOnce(t *testing.T) {
	setupLinkMemo(t, "example.com/extra", "example.invalid/bad")
	resolver := &countingResolver{dir: t.TempDir(), bad: map[string]bool{"example.invalid/bad": true}}
	resolver.install(t)

	cfg := newLinkConfig()
	_, err := resolveRuleAddedImports(t.Context(), nil, cfg)
	require.NoError(t, err)
	require.Equal(t, 1, resolver.calls)
	assert.NotContains(t, cfg.PackageFile, "example.invalid/bad")

	// A later link reads the failure from the memo too.
	_, err = resolveRuleAddedImports(t.Context(), nil, newLinkConfig())
	require.NoError(t, err)
	assert.Equal(t, 1, resolver.calls)
}

func TestLinkResolutionSkipsGoListWhenNothingIsMissing(t *testing.T) {
	setupLinkMemo(t, "example.com/extra")
	resolver := &countingResolver{dir: t.TempDir()}
	resolver.install(t)

	cfg := newLinkConfig()
	cfg.PackageFile["example.com/extra"] = "/unused/extra.a"
	updated, err := resolveRuleAddedImports(t.Context(), nil, cfg)
	require.NoError(t, err)
	assert.False(t, updated)
	assert.Zero(t, resolver.calls)
}

func TestLinkResolutionWithoutSessionResolvesEveryLink(t *testing.T) {
	setupLinkMemo(t, "example.com/extra")
	t.Setenv(util.EnvOtelcSessionDir, util.SessionOff)
	resolver := &countingResolver{dir: t.TempDir()}
	resolver.install(t)

	for range 2 {
		_, err := resolveRuleAddedImports(t.Context(), nil, newLinkConfig())
		require.NoError(t, err)
	}
	assert.Equal(t, 2, resolver.calls)
}
