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
