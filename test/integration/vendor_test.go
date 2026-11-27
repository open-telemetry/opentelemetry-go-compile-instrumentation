// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
)

func TestVendoredBuild(t *testing.T) {
	t.Parallel()

	app := copyTestApp(t, "vendored")
	appsDir := filepath.Dir(app)
	vendoredApp := filepath.Base(app)

	goMod(t, app, "vendor")
	modulesTxt := filepath.Join(app, "vendor", "modules.txt")
	before, err := os.ReadFile(modulesTxt)
	require.NoError(t, err)

	// setup edits go.mod but not vendor/modules.txt, so the build needs -mod=mod
	// to pass the vendor consistency check.
	testutil.Build(t, appsDir, vendoredApp, "go", "build")

	binPath := filepath.Join(app, "app")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}
	cmd := exec.CommandContext(t.Context(), binPath)
	cmd.Dir = app
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Contains(t, string(out), "vendored hook executed")
	require.Contains(t, string(out), "rate limit: 1")

	after, err := os.ReadFile(modulesTxt)
	require.NoError(t, err)
	require.Equal(t, before, after, "otelc must not modify the vendor directory")
}

func TestYAMLSelectionVendoredBuild(t *testing.T) {
	app := copyTestApp(t, "vendored")
	appsDir := filepath.Dir(app)
	appName := filepath.Base(app)

	goMod(t, app, "vendor")
	modulesTxt := filepath.Join(app, "vendor", "modules.txt")
	beforeVendor, err := os.ReadFile(modulesTxt)
	require.NoError(t, err)
	goModPath := filepath.Join(app, "go.mod")
	beforeMod, err := os.ReadFile(goModPath)
	require.NoError(t, err)
	goSumPath := filepath.Join(app, "go.sum")
	beforeSum, err := os.ReadFile(goSumPath)
	require.NoError(t, err)

	toolFile := filepath.Join(app, toolFileCanonical)
	require.NoError(t, os.Remove(toolFile))
	yamlPath := filepath.Join(app, "otel.instrumentation.yml")
	yamlContent := []byte("instrumentations:\n  - go.opentelemetry.io/otelc/test/apps/vendored/hooks\n")
	require.NoError(t, os.WriteFile(yamlPath, yamlContent, 0o644))

	testutil.Build(t, appsDir, appName, "go", "build")
	output := testutil.Run(t, appsDir, appName, nil)
	require.Contains(t, output, "vendored hook executed")
	require.Contains(t, output, "rate limit: 1")

	afterYAML, err := os.ReadFile(yamlPath)
	require.NoError(t, err)
	assert.Equal(t, yamlContent, afterYAML)
	afterMod, err := os.ReadFile(goModPath)
	require.NoError(t, err)
	assert.Equal(t, beforeMod, afterMod)
	afterSum, err := os.ReadFile(goSumPath)
	require.NoError(t, err)
	assert.Equal(t, beforeSum, afterSum)
	afterVendor, err := os.ReadFile(modulesTxt)
	require.NoError(t, err)
	assert.Equal(t, beforeVendor, afterVendor)
	assert.NoFileExists(t, toolFile)
}

func goMod(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "go", append([]string{"mod"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}
