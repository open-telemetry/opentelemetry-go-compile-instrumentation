// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
	"go.opentelemetry.io/otelc/tool/util"
)

// TestBuildRemovesGoWorkDir guards against leaking Go's temporary work directory outside debug mode.
func TestBuildRemovesGoWorkDir(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)
	t.Setenv("GOFLAGS", "")
	t.Setenv(util.EnvOtelcDebug, "")

	for _, tt := range []struct {
		name  string
		debug bool
	}{
		{name: "default"},
		{name: "debug", debug: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			moduleDir := t.TempDir()
			writeTestFile(t, moduleDir, "go.mod", "module example.com/otelc-work-dir\n\ngo 1.25.0\n")
			writeTestFile(t, moduleDir, "main.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "net/http"

func main() {
	_, _ = http.Get("http://127.0.0.1:1/")
}
`)
			tmpDir, err := filepath.EvalSymlinks(t.TempDir())
			require.NoError(t, err)
			t.Setenv("GOTMPDIR", tmpDir)
			binary := filepath.Join(moduleDir, "app")
			args := []string{"go", "build", "-o", binary, "."}
			if tt.debug {
				args = append([]string{"--debug"}, args...)
			}
			output := runOtelcCommand(t, moduleDir, os.Environ(), otelcPath, args...)
			workLine := regexp.MustCompile(`(?m)^WORK=([^\r\n]+)\r?$`).FindStringSubmatch(output)
			if tt.debug {
				require.Len(t, workLine, 2, "debug build did not print its work directory: %s", output)
				require.Equal(t, tmpDir, filepath.Dir(workLine[1]))
				require.DirExists(t, workLine[1])
			} else {
				require.Empty(t, workLine, "default build printed a WORK line: %s", output)
				workDirs, globErr := filepath.Glob(filepath.Join(tmpDir, "go-build*"))
				require.NoError(t, globErr)
				require.Empty(t, workDirs, "default build left Go work directories behind")
			}
			symbols := goOutput(t, moduleDir, "tool", "nm", binary)
			require.Contains(t, symbols, "otelc/instrumentation/net/http", "net/http was not instrumented")
		})
	}
}
