// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
)

func TestExplicitInstrumentationSelection(t *testing.T) {
	t.Parallel()

	app := copyTestApp(t, "customrules")
	appsDir := filepath.Dir(app)
	appName := filepath.Base(app)

	testutil.Build(t, appsDir, appName, "go", "build", "-a")

	matchedPath := filepath.Join(app, ".otelc-build", "matched.json")
	require.FileExists(t, matchedPath)

	raw, err := os.ReadFile(matchedPath)
	require.NoError(t, err)

	// Verify recursive composition: both intermediate and base rules were discovered and matched.
	assert.Contains(t, string(raw), "go.opentelemetry.io/otelc/test/apps/customrules/instrumentation",
		"intermediate instrumentation rule must be selected")
	assert.Contains(t, string(raw), "go.opentelemetry.io/otelc/test/apps/customrules/base",
		"recursively composed base instrumentation rule must be selected")

	binPath := filepath.Join(app, "app")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}
	cmd := exec.CommandContext(t.Context(), binPath)
	cmd.Dir = app
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	output := string(out)
	assert.Contains(t, output, "custom hook called")
	assert.Contains(t, output, "base hook called")
	assert.Contains(t, output, "custom target called")
	assert.Contains(t, output, "base target called")
}

func TestYAMLInstrumentationSelectionBuildRestoresModule(t *testing.T) {
	t.Parallel()

	app := copyTestApp(t, "customrules")
	appsDir := filepath.Dir(app)
	appName := filepath.Base(app)
	toolFile := filepath.Join(app, "otel.instrumentation.go")
	require.NoError(t, os.Remove(toolFile))

	yamlPath := filepath.Join(app, "otel.instrumentation.yml")
	yamlContent := []byte("instrumentations:\n  - go.opentelemetry.io/otelc/test/apps/customrules/instrumentation\n")
	require.NoError(t, os.WriteFile(yamlPath, yamlContent, 0o644))
	goModPath := filepath.Join(app, "go.mod")
	beforeMod, err := os.ReadFile(goModPath)
	require.NoError(t, err)
	goSumPath := filepath.Join(app, "go.sum")
	beforeSum, sumErr := os.ReadFile(goSumPath)
	sumExisted := sumErr == nil
	if sumErr != nil {
		require.True(t, os.IsNotExist(sumErr), "read go.sum: %v", sumErr)
	}

	testutil.Build(t, appsDir, appName, "go", "build", "-a")

	matchedPath := filepath.Join(app, ".otelc-build", "matched.json")
	require.FileExists(t, matchedPath)
	matched, err := os.ReadFile(matchedPath)
	require.NoError(t, err)
	assert.Contains(t, string(matched), "go.opentelemetry.io/otelc/test/apps/customrules/instrumentation")
	assert.Contains(t, string(matched), "go.opentelemetry.io/otelc/test/apps/customrules/base")

	binPath := filepath.Join(app, "app")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}
	cmd := exec.CommandContext(t.Context(), binPath)
	cmd.Dir = app
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	assert.Contains(t, string(output), "custom hook called")
	assert.Contains(t, string(output), "base hook called")

	afterYAML, err := os.ReadFile(yamlPath)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(yamlContent, afterYAML), "AutoPin must leave the YAML file unchanged")
	afterMod, err := os.ReadFile(goModPath)
	require.NoError(t, err)
	assert.Equal(t, beforeMod, afterMod)
	afterSum, afterSumErr := os.ReadFile(goSumPath)
	assert.Equal(t, sumExisted, afterSumErr == nil)
	if sumExisted {
		assert.Equal(t, beforeSum, afterSum)
	} else {
		require.ErrorIs(t, afterSumErr, os.ErrNotExist)
	}
	assert.NoFileExists(t, toolFile)
}

func TestEmptyYAMLInstrumentationSelectionSuppressesManifestDefaults(t *testing.T) {
	app := copyTestApp(t, "injecteddeps")
	appsDir := filepath.Dir(app)
	appName := filepath.Base(app)
	toolFile := filepath.Join(app, "otel.instrumentation.go")
	require.NoError(t, os.Remove(toolFile))
	yamlPath := filepath.Join(app, "otel.instrumentation.yml")
	yamlContent := []byte("instrumentations: []\n")
	require.NoError(t, os.WriteFile(yamlPath, yamlContent, 0o644))
	goModPath := filepath.Join(app, "go.mod")
	beforeMod, err := os.ReadFile(goModPath)
	require.NoError(t, err)
	goSumPath := filepath.Join(app, "go.sum")
	beforeSum, sumErr := os.ReadFile(goSumPath)
	sumExisted := sumErr == nil
	if sumErr != nil {
		require.True(t, os.IsNotExist(sumErr), "read go.sum: %v", sumErr)
	}

	testutil.Build(t, appsDir, appName, "go", "build", "-a")

	matchedPath := filepath.Join(app, ".otelc-build", "matched.json")
	require.FileExists(t, matchedPath)
	matched, err := os.ReadFile(matchedPath)
	require.NoError(t, err)
	assert.Equal(t, []byte("null"), bytes.TrimSpace(matched),
		"an explicit empty selection must suppress manifest-based matching")

	output := testutil.Run(t, appsDir, appName, nil)
	assert.Contains(t, output, "injecteddeps: done")
	assert.NotContains(t, output, "target.Instrumented")

	afterYAML, err := os.ReadFile(yamlPath)
	require.NoError(t, err)
	assert.Equal(t, yamlContent, afterYAML)
	afterMod, err := os.ReadFile(goModPath)
	require.NoError(t, err)
	assert.Equal(t, beforeMod, afterMod)
	afterSum, afterSumErr := os.ReadFile(goSumPath)
	assert.Equal(t, sumExisted, afterSumErr == nil)
	if sumExisted {
		assert.Equal(t, beforeSum, afterSum)
	} else {
		require.ErrorIs(t, afterSumErr, os.ErrNotExist)
	}
	assert.NoFileExists(t, toolFile)
}

func TestYAMLInstrumentationSelectionPrunesInvalidImportAndRestoresModule(t *testing.T) {
	app := copyTestApp(t, "customrules")
	appsDir := filepath.Dir(app)
	appName := filepath.Base(app)
	toolFile := filepath.Join(app, "otel.instrumentation.go")
	require.NoError(t, os.Remove(toolFile))
	yamlPath := filepath.Join(app, "otel.instrumentation.yml")
	yamlContent := []byte(`instrumentations:
  - go.opentelemetry.io/otelc/test/apps/customrules/instrumentation
  - example.com/uninstrumented
`)
	require.NoError(t, os.WriteFile(yamlPath, yamlContent, 0o644))
	goModPath := filepath.Join(app, "go.mod")
	beforeMod, err := os.ReadFile(goModPath)
	require.NoError(t, err)
	goSumPath := filepath.Join(app, "go.sum")
	beforeSum, sumErr := os.ReadFile(goSumPath)
	sumExisted := sumErr == nil
	if sumErr != nil {
		require.True(t, os.IsNotExist(sumErr), "read go.sum: %v", sumErr)
	}

	testutil.Build(t, appsDir, appName, "go", "build", "-a")

	matchedPath := filepath.Join(app, ".otelc-build", "matched.json")
	require.FileExists(t, matchedPath)
	matched, err := os.ReadFile(matchedPath)
	require.NoError(t, err)
	assert.Contains(t, string(matched), "go.opentelemetry.io/otelc/test/apps/customrules/instrumentation")
	assert.Contains(t, string(matched), "go.opentelemetry.io/otelc/test/apps/customrules/base")
	assert.NotContains(t, string(matched), "example.com/uninstrumented")

	output := testutil.Run(t, appsDir, appName, nil)
	assert.Contains(t, output, "custom hook called")
	assert.Contains(t, output, "base hook called")

	afterYAML, err := os.ReadFile(yamlPath)
	require.NoError(t, err)
	assert.Equal(t, yamlContent, afterYAML, "AutoPin must leave the YAML file unchanged")
	afterMod, err := os.ReadFile(goModPath)
	require.NoError(t, err)
	assert.Equal(t, beforeMod, afterMod)
	afterSum, afterSumErr := os.ReadFile(goSumPath)
	assert.Equal(t, sumExisted, afterSumErr == nil)
	if sumExisted {
		assert.Equal(t, beforeSum, afterSum)
	} else {
		require.ErrorIs(t, afterSumErr, os.ErrNotExist)
	}
	assert.NoFileExists(t, toolFile)
}
