// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/session"
	"go.opentelemetry.io/otelc/tool/util"
)

func TestSessionOwnership(t *testing.T) {
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	t.Setenv(util.EnvOtelcSessionDir, "")
	owned := util.GetBuildTemp(util.SessionTempDir)
	require.NoError(t, os.MkdirAll(owned, 0o755))

	env := sessionEnv(nil)
	assert.Equal(t, []string{util.EnvOtelcSessionDir + "=" + owned}, env)
	require.NoError(t, cleanupOwnedSession())
	assert.NoDirExists(t, owned, "a build that owns its session cleans it")

	// A build that inherits a session neither re-exports nor cleans it.
	inherited := t.TempDir()
	t.Setenv(util.EnvOtelcSessionDir, inherited)
	assert.Empty(t, sessionEnv(nil))
	require.NoError(t, cleanupOwnedSession())
	assert.DirExists(t, inherited)
}

func TestResolutionScopeTracksModuleFiles(t *testing.T) {
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
	modDir := t.TempDir()
	mustWriteFile(t, filepath.Join(modDir, "go.mod"), "module example.com/scope\n\ngo 1.24\n")

	scope := func() string {
		t.Helper()
		require.NoError(t, writeResolutionScope(t.Context(), []string{modDir}))
		content, err := os.ReadFile(util.GetResolutionScopeFile())
		require.NoError(t, err)
		return string(content)
	}
	first := scope()
	assert.Equal(t, first, scope(), "the scope is deterministic")

	mustWriteFile(t, filepath.Join(modDir, "go.mod"), "module example.com/scope\n\ngo 1.25\n")
	assert.NotEqual(t, first, scope(), "an edited go.mod changes the scope")

	second := scope()
	t.Setenv("GOARCH", "arm64")
	assert.NotEqual(t, second, scope(), "another GOARCH changes the scope")
}

// TestResolutionScopeTracksImplicitWorkspace covers a go.work that go finds
// in a parent directory of the module: the module's own files are the same,
// but the workspace decides which dependencies resolve, so builds with and
// without it, or with another go.work.sum, must not share memo entries.
func TestResolutionScopeTracksImplicitWorkspace(t *testing.T) {
	t.Setenv("GOWORK", "")
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	t.Setenv(util.EnvOtelcSessionDir, t.TempDir())
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
	root := t.TempDir()
	modDir := filepath.Join(root, "mod")
	mustWriteFile(t, filepath.Join(modDir, "go.mod"), "module example.com/scope\n\ngo 1.24\n")

	scope := func() string {
		t.Helper()
		require.NoError(t, writeResolutionScope(t.Context(), []string{modDir}))
		content, err := os.ReadFile(util.GetResolutionScopeFile())
		require.NoError(t, err)
		return string(content)
	}
	archive := filepath.Join(t.TempDir(), "a.a")
	mustWriteFile(t, archive, "a")

	alone := scope()
	session.Open(nil).Store("example.com/a", map[string]string{"example.com/a": archive}, nil)
	_, _, ok := session.Open(nil).Lookup("example.com/a", nil)
	require.True(t, ok)

	mustWriteFile(t, filepath.Join(root, "go.work"), "go 1.24\n\nuse ./mod\n")
	inWorkspace := scope()
	assert.NotEqual(t, alone, inWorkspace, "an implicit parent go.work changes the scope")
	_, _, ok = session.Open(nil).Lookup("example.com/a", nil)
	assert.False(t, ok, "a build in the workspace must not reuse entries from a build outside it")

	mustWriteFile(t, filepath.Join(root, "go.work.sum"), "example.com/x v1.0.0 h1:abc=\n")
	assert.NotEqual(t, inWorkspace, scope(), "an edited go.work.sum changes the scope")
}
