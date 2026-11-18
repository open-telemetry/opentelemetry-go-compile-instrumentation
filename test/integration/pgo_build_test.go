// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
	"go.opentelemetry.io/otelc/tool/util"
)

// TestPGOBuildIsInstrumented builds an instrumented application in a module
// directory that contains a default.pgo profile. Go picks up default.pgo
// automatically and passes -pgo=<path> to the compiler; otelc must preserve
// that flag when invoking the compiler for instrumented packages.
func TestPGOBuildIsInstrumented(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)

	moduleDir := copyTestApp(t, "pgo")

	profile := filepath.Join(moduleDir, "default.pgo")
	profileCmd := exec.CommandContext(t.Context(), "go", "run", ".", "profile", profile)
	profileCmd.Dir = moduleDir
	out, err := profileCmd.CombinedOutput()
	require.NoError(t, err, "profile generation failed:\n%s", out)
	require.FileExists(t, profile, "CPU profile was not generated")

	binary := filepath.Join(moduleDir, "app")
	if util.IsWindows() {
		binary += ".exe"
	}
	runOtelcCommand(t, moduleDir, os.Environ(), otelcPath, "go", "build", "-o", binary, ".")

	symbols := goOutput(t, moduleDir, "tool", "nm", binary)
	require.Contains(t, symbols, "go.opentelemetry.io/otelc/test/apps/pgo/hooks.Before",
		"hook was not instrumented in a build with default.pgo")

	buildInfo := goOutput(t, moduleDir, "version", "-m", binary)
	require.Contains(t, buildInfo, "-pgo="+profile, "the build no longer applies the PGO profile")

	run := exec.CommandContext(t.Context(), binary)
	run.Dir = moduleDir
	runOut, runErr := run.CombinedOutput()
	require.NoError(t, runErr, string(runOut))
	require.Contains(t, string(runOut), "hook executed")
}
