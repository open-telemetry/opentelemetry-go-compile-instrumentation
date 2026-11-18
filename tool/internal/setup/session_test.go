// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"os"
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
