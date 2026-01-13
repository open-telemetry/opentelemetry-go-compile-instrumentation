// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

const (
	otelBinName = "otel"
	appBinName  = "app"
)

// -----------------------------------------------------------------------------
// E2E Test Infrastructure
// This infrastructure is used to actually build the application with the otel
// instrumentation tool, execute the application and verify the output.

func newCmd(ctx context.Context, dir string, args ...string) *exec.Cmd {
	path := args[0]
	args = args[1:]
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	return cmd
}

// Build builds the application with the instrumentation tool.
func Build(t *testing.T, appDir string, args ...string) {
	binName := otelBinName
	if util.IsWindows() {
		binName += ".exe"
	}
	pwd, err := os.Getwd()
	require.NoError(t, err)
	otelPath := filepath.Join(pwd, "..", "..", binName)

	// Use a consistent binary name for all test apps
	outputName := appBinName
	if util.IsWindows() {
		outputName += ".exe"
	}

	// Insert -o flag after "build" in the args
	// args typically contains ["go", "build", "-a"], we want ["go", "build", "-o", outputName, "-a"]
	newArgs := make([]string, 0, len(args)+2)
	for _, arg := range args {
		newArgs = append(newArgs, arg)
		if arg == "build" {
			// Insert -o flag right after "build"
			newArgs = append(newArgs, "-o", outputName)
		}
	}

	args = append([]string{otelPath}, newArgs...)

	cmd := newCmd(t.Context(), appDir, args...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	t.Cleanup(func() {
		os.Remove(filepath.Join(appDir, outputName))
	})
}

// Run runs the application and returns the output.
// It waits for the application to complete.
func Run(t *testing.T, dir string, args ...string) string {
	appName := "./" + appBinName
	if util.IsWindows() {
		appName += ".exe"
	}
	cmd := newCmd(t.Context(), dir, append([]string{appName}, args...)...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return string(out)
}

// Start starts the application but does not wait for it to complete.
// It returns the command and the combined output pipe(stdout and stderr).
func Start(t *testing.T, dir string, args ...string) {
	appName := "./" + appBinName
	if util.IsWindows() {
		appName += ".exe"
	}
	cmd := newCmd(t.Context(), dir, append([]string{appName}, args...)...)
	cmd.Stderr = cmd.Stdout // redirect stderr to stdout for easier debugging
	err := cmd.Start()
	require.NoError(t, err)
	t.Cleanup(func() {
		if cmd.Process != nil && cmd.ProcessState == nil {
			require.NoError(t, cmd.Process.Kill())
		}
	})
}
