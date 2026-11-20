// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	writeModule(t, root, "instrumentation/example", "example.com/instrumentation/example")
	writeRuleFile(t, root, "instrumentation/example/otelc.yaml", `
http:
  target: net/http
  version: v1.0.0
`)
	require.NoError(t, os.MkdirAll(filepath.Join("tool", "data"), 0o755))

	origFetch := fetchRepo
	fetchRepo = func(version string) (string, string, func(), error) {
		return "v1.0.0", root, func() {}, nil
	}
	t.Cleanup(func() { fetchRepo = origFetch })

	require.NoError(t, run())

	content, err := os.ReadFile(filepath.Join("tool", "data", "instrumentation-manifest.json"))
	require.NoError(t, err)
	require.NotEmpty(t, content)
	require.Equal(t, byte('\n'), content[len(content)-1])
	require.True(t, json.Valid(content))

	var got Manifest
	require.NoError(t, json.Unmarshal(content, &got))
	require.Equal(t, Manifest{{
		ModulePath:   "example.com/instrumentation/example",
		Target:       "net/http",
		VersionRange: "v1.0.0",
		Version:      "v1.0.0",
	}}, got)
}

func TestRunFetchError(t *testing.T) {
	origFetch := fetchRepo
	fetchRepo = func(version string) (string, string, func(), error) {
		return "", "", nil, errors.New("simulated fetch failure")
	}
	t.Cleanup(func() { fetchRepo = origFetch })

	err := run()
	require.ErrorContains(t, err, "simulated fetch failure")
}

func TestRunGenerateError(t *testing.T) {
	origFetch := fetchRepo
	fetchRepo = func(version string) (string, string, func(), error) {
		return "v1.0.0", t.TempDir(), func() {}, nil
	}
	t.Cleanup(func() { fetchRepo = origFetch })

	err := run()
	require.ErrorContains(t, err, "generate instrumentation manifest")
}

func TestRunWriteError(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	writeModule(t, root, "instrumentation/example", "example.com/instrumentation/example")
	writeRuleFile(t, root, "instrumentation/example/otelc.yaml", `
http:
  target: net/http
`)

	origFetch := fetchRepo
	fetchRepo = func(version string) (string, string, func(), error) {
		return "v1.0.0", root, func() {}, nil
	}
	t.Cleanup(func() { fetchRepo = origFetch })

	err := run()
	require.ErrorContains(t, err, "write instrumentation manifest")
}

func TestRunCustomFlags(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	writeModule(t, root, "instrumentation/example", "example.com/instrumentation/example")
	writeRuleFile(t, root, "instrumentation/example/otelc.yaml", `
http:
  target: net/http
`)
	outDir := filepath.Join(root, "custom")
	require.NoError(t, os.MkdirAll(outDir, 0o755))
	outFile := filepath.Join(outDir, "custom-manifest.json")

	var capturedVersion string
	origFetch := fetchRepo
	fetchRepo = func(version string) (string, string, func(), error) {
		capturedVersion = version
		return "v2.0.0", root, func() {}, nil
	}
	t.Cleanup(func() { fetchRepo = origFetch })

	require.NoError(t, run("-version", "v2.0.0", "-output", outFile))
	require.Equal(t, "v2.0.0", capturedVersion)

	content, err := os.ReadFile(outFile)
	require.NoError(t, err)

	var got Manifest
	require.NoError(t, json.Unmarshal(content, &got))
	require.Equal(t, Manifest{{
		ModulePath: "example.com/instrumentation/example",
		Target:     "net/http",
		Version:    "v2.0.0",
	}}, got)
}

func TestRunInvalidFlag(t *testing.T) {
	err := run("-nonexistent-flag")
	require.Error(t, err)
}
