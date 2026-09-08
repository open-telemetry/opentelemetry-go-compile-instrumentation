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

const (
	originalSource     = "package main\n\nfunc main() {}\n"
	instrumentedSource = "package main\n\nfunc main() { println(\"hi\") }\n"
)

// sourcePair writes an original and an instrumented copy of the same file
// name into sibling directories, mirroring how the instrument phase leaves
// the original in place and writes its rewrite into the work directory.
func sourcePair(t *testing.T, instrumented string) (oldFile, newFile string) {
	t.Helper()
	oldDir, newDir := t.TempDir(), t.TempDir()
	oldFile = filepath.Join(oldDir, "source.go")
	newFile = filepath.Join(newDir, "source.go")
	require.NoError(t, os.WriteFile(oldFile, []byte(originalSource), 0o644))
	require.NoError(t, os.WriteFile(newFile, []byte(instrumented), 0o644))
	return oldFile, newFile
}

func TestWriteDiffForDebugWritesDiffUnderPackageDir(t *testing.T) {
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	t.Setenv(util.EnvOtelcDebug, "1")
	oldFile, newFile := sourcePair(t, instrumentedSource)

	ip := newTestPhase()
	ip.compileArgs = []string{"-p", "github.com/redis/go-redis/v9"}
	ip.writeDiffForDebug(oldFile, newFile)

	dest := util.GetBuildTemp(filepath.Join("debug", "github_com_redis_go-redis_v9", "source.go.diff"))
	content, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Contains(t, string(content), "-func main() {}")
	assert.Contains(t, string(content), "+func main() { println(\"hi\") }")
}

func TestWriteDiffForDebugSkips(t *testing.T) {
	tests := []struct {
		name         string
		debug        string
		instrumented string
	}{
		{name: "debug unset", debug: "", instrumented: instrumentedSource},
		{name: "debug explicitly off", debug: "0", instrumented: instrumentedSource},
		{name: "content unchanged", debug: "1", instrumented: originalSource},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
			t.Setenv(util.EnvOtelcDebug, test.debug)
			oldFile, newFile := sourcePair(t, test.instrumented)

			newTestPhase().writeDiffForDebug(oldFile, newFile)

			_, err := os.Stat(util.GetBuildTemp(filepath.Join("debug", "source.go.diff")))
			assert.True(t, os.IsNotExist(err), "expected no diff file, stat returned %v", err)
		})
	}
}
