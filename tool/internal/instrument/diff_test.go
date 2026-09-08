// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/util"
)

func writeTempFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func TestWriteDiffForDebug(t *testing.T) {
	dir := t.TempDir()
	oldFile := writeTempFile(t, dir, "source.go", "package main\n\nfunc main() {}\n")
	newFile := writeTempFile(t, dir, "source.go.new", "package main\n\nfunc main() { println(\"hi\") }\n")

	t.Run("writes a diff when debug is on and content differs", func(t *testing.T) {
		t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
		t.Setenv(util.EnvOtelcDebug, "1")

		newTestPhase().writeDiffForDebug(oldFile, newFile)

		dest := util.GetBuildTemp(filepath.Join("debug", filepath.Base(oldFile)+".diff"))
		content, err := os.ReadFile(dest)
		require.NoError(t, err)
		assert.Contains(t, string(content), "-func main() {}")
		assert.Contains(t, string(content), "+func main() { println(\"hi\") }")
	})

	t.Run("does nothing when debug is off", func(t *testing.T) {
		t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
		t.Setenv(util.EnvOtelcDebug, "")

		newTestPhase().writeDiffForDebug(oldFile, newFile)

		dest := util.GetBuildTemp(filepath.Join("debug", filepath.Base(oldFile)+".diff"))
		_, err := os.Stat(dest)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("does nothing when content is identical", func(t *testing.T) {
		t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
		t.Setenv(util.EnvOtelcDebug, "1")

		newTestPhase().writeDiffForDebug(oldFile, oldFile)

		dest := util.GetBuildTemp(filepath.Join("debug", filepath.Base(oldFile)+".diff"))
		_, err := os.Stat(dest)
		assert.True(t, os.IsNotExist(err))
	})
}
