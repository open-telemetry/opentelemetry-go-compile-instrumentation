//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
)

func TestApplicationConfigurationSnapshotIsEmbeddedAndCacheAware(t *testing.T) {
	appDir := copyTestApp(t, "configuration")
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	goCache := filepath.Join(t.TempDir(), "gocache")
	require.NoError(t, os.MkdirAll(goCache, 0o755))

	build := func(withApplicationFile, enabled bool, runtimeOverride string) string {
		t.Helper()
		value := "false"
		if enabled {
			value = "true"
		}
		configPath := filepath.Join(appDir, "application.yaml")
		if withApplicationFile {
			config := "file_format: \"1.2\"\ninstrumentation/development:\n  go:\n    configuration:\n      enabled: " + value + "\n"
			writeTestFile(t, appDir, "application.yaml", config)
		} else {
			_ = os.Remove(configPath)
		}
		debugLog := filepath.Join(appDir, ".otelc-build", "debug.log")
		_ = os.Remove(debugLog)

		env := append(os.Environ(),
			"GOCACHE="+goCache,
			"GOWORK=off",
			"OTELC_STATS=1",
			"OTEL_INSTRUMENTATION_GO_CONFIGURATION_ENABLED="+runtimeOverride,
		)
		args := []string{"--rules", "hooks/otelc.yaml"}
		if withApplicationFile {
			args = append(args, "--instrumentation-config", "application.yaml")
		}
		args = append(args, "go", "build", "-o", "app", ".")
		runOtelcCommand(t, appDir, env, otelcPath, args...)
		log, readErr := os.ReadFile(debugLog)
		require.NoError(t, readErr)
		return string(log)
	}

	assertSnapshot := func(enabled bool, runtimeOverride string) {
		t.Helper()
		want := `{"version":1,"values":{"go.configuration.enabled":` + map[bool]string{true: "true", false: "false"}[enabled] + `}}`
		runtimeEnv := append(os.Environ(), "OTEL_INSTRUMENTATION_GO_CONFIGURATION_ENABLED="+runtimeOverride)
		output := testutil.Run(t, filepath.Dir(appDir), filepath.Base(appDir), runtimeEnv)
		assert.Contains(t, output, "dependency-snapshot:"+want,
			"the snapshot must be available to dependency init functions")
		assert.Contains(t, output, "hook-snapshot:"+want,
			"the snapshot must be available to generated instrumentation hooks")
		assert.Contains(t, output, "main-snapshot:"+want)
	}
	compiledMain := func(log string) bool {
		for _, line := range strings.Split(log, "\n") {
			if strings.Contains(line, "toolexec stats") &&
				strings.Contains(line, "tool=compile") && strings.Contains(line, "package=main") {
				return true
			}
		}
		return false
	}

	coldLog := build(true, true, "false")
	assert.True(t, compiledMain(coldLog), "cold build should compile the main package through toolexec")
	assertSnapshot(true, "false")

	// Process-start override values are deliberately not an input to setup or
	// the compile cache identity. #1492 will resolve them at runtime.
	warmLog := build(true, true, "true")
	assert.False(t, compiledMain(warmLog), "identical build inputs should reuse the cached main-package compile")
	assertSnapshot(true, "true")

	changedLog := build(true, false, "true")
	assert.True(t, compiledMain(changedLog),
		"changing a selected configuration value must invalidate the compile identity")
	assertSnapshot(false, "true")

	defaultLog := build(false, false, "true")
	assert.False(t, compiledMain(defaultLog),
		"omitting the application file should embed the same metadata default without recompiling")
	assertSnapshot(false, "true")

	writeTestFile(t, appDir, "metadata.yaml", `configurations:
  - name: go.configuration.enabled
    type: boolean
    default: "true"
`)
	metadataLog := build(false, false, "true")
	assert.True(t, compiledMain(metadataLog), "changing a selected metadata default must invalidate the cache")
	assertSnapshot(true, "true")
}
