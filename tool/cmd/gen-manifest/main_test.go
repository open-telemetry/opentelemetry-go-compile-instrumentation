// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/manifest"
)

func TestRun(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	writeModule(t, "instrumentation/example", "example.com/instrumentation/example")
	writeFile(t, "instrumentation/example/otelc.yaml", `
http:
  target: net/http
  version: v1.0.0
`)
	require.NoError(t, os.MkdirAll(filepath.Join("tool", "data"), 0o755))

	require.NoError(t, run())

	content, err := os.ReadFile(filepath.Join("tool", "data", "instrumentation-manifest.json"))
	require.NoError(t, err)
	require.NotEmpty(t, content)
	require.Equal(t, byte('\n'), content[len(content)-1])
	require.True(t, json.Valid(content))

	var got manifest.Manifest
	require.NoError(t, json.Unmarshal(content, &got))
	require.Equal(t, manifest.Manifest{{
		ModulePath:   "example.com/instrumentation/example",
		Target:       "net/http",
		VersionRange: "v1.0.0",
	}}, got)
}

func TestRunGenerateError(t *testing.T) {
	t.Chdir(t.TempDir())

	err := run()
	require.ErrorContains(t, err, "generate instrumentation manifest")
}

func TestRunWriteError(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	writeModule(t, "instrumentation/example", "example.com/instrumentation/example")
	writeFile(t, "instrumentation/example/otelc.yaml", `
http:
  target: net/http
`)

	err := run()
	require.ErrorContains(t, err, "write instrumentation manifest")
}

func writeModule(t *testing.T, relative, modulePath string) {
	t.Helper()
	writeFile(t, filepath.Join(relative, "go.mod"), "module "+modulePath+"\n")
}

func writeFile(t *testing.T, relative, content string) {
	t.Helper()
	path := filepath.FromSlash(relative)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}
