// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/util"
)

func TestLoadMissingMatchedRules(t *testing.T) {
	// Point the work dir at an empty directory: matched.json does not exist,
	// which is what a bare -toolexec build sees when setup never ran.
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())

	ip := &instrumentPhase{logger: slog.Default()}
	_, err := ip.load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "otelc setup")
}

func TestLoadImportNames_MissingFileIsNotAnError(t *testing.T) {
	// import_names.json only improves a guess. An older setup run
	// without the file must still succeed.
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())

	names, err := loadImportNames()
	require.NoError(t, err)
	assert.Empty(t, names)
}

func TestLoadImportNames_ValidFile(t *testing.T) {
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
	require.NoError(t, os.WriteFile(
		util.GetImportNamesFile(),
		[]byte(`{"github.com/redis/go-redis/v9":"redis"}`),
		0o644,
	))

	names, err := loadImportNames()
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"github.com/redis/go-redis/v9": "redis"}, names)
}

func TestLoadImportNames_MalformedFile(t *testing.T) {
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
	require.NoError(t, os.WriteFile(util.GetImportNamesFile(), []byte("not json"), 0o644))

	_, err := loadImportNames()
	require.Error(t, err)
}

func TestLoadImportNames_ReadErrorOtherThanMissing(t *testing.T) {
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	require.NoError(t, os.MkdirAll(util.GetImportNamesFile(), 0o755))

	_, err := loadImportNames()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read file")
}
