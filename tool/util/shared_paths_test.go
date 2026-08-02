// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetOtelcWorkDir(t *testing.T) {
	t.Run("uses OTELC_WORK_DIR when set", func(t *testing.T) {
		t.Setenv(EnvOtelcWorkDir, "/tmp/otelc-work")
		assert.Equal(t, "/tmp/otelc-work", GetOtelcWorkDir())
	})
	t.Run("falls back to cwd when unset", func(t *testing.T) {
		t.Setenv(EnvOtelcWorkDir, "")
		cwd, err := os.Getwd()
		require.NoError(t, err)
		assert.Equal(t, cwd, GetOtelcWorkDir())
	})
}

func TestGetBuildTempPaths(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(EnvOtelcWorkDir, workDir)

	assert.Equal(t, filepath.Join(workDir, BuildTempDir), GetBuildTempDir())
	assert.Equal(t, filepath.Join(workDir, BuildTempDir, "foo.txt"), GetBuildTemp("foo.txt"))
	assert.Equal(t, filepath.Join(workDir, BuildTempDir, "matched.json"), GetMatchedRuleFile())
	assert.Equal(t, filepath.Join(workDir, BuildTempDir, "added_imports.*.json"), GetAddedImportsPattern())

	// The per-process import file embeds the current PID.
	want := filepath.Join(workDir, BuildTempDir, fmt.Sprintf("added_imports.%d.json", os.Getpid()))
	assert.Equal(t, want, GetAddedImportsFileForProcess())
}

func TestEncodeBuildFlagsEmpty(t *testing.T) {
	assert.Empty(t, EncodeBuildFlags(nil))
	assert.Empty(t, EncodeBuildFlags([]string{}))
}

func TestEncodeBuildFlagsRoundTrip(t *testing.T) {
	flags := []string{"-tags", "foo bar", "-race"}
	encoded := EncodeBuildFlags(flags)
	assert.NotEmpty(t, encoded)
	// The encoding must be valid JSON that preserves spaces in tokens.
	assert.True(t, strings.HasPrefix(encoded, "["))
	assert.Contains(t, encoded, "foo bar")
}
