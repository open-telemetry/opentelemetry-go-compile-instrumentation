// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
		require.NoError(t, writeResolutionScope([]string{modDir}))
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
