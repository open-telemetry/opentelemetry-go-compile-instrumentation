// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
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
	return cmd
}

// Build builds the application with the instrumentation tool.
func Build(t *testing.T, appDir string, args ...string) {
	binName := "otel"
	if util.IsWindows() {
		binName += ".exe"
	}
	pwd, err := os.Getwd()
	require.NoError(t, err)
	otelPath := filepath.Join(pwd, "..", "..", binName)
	args = append([]string{otelPath}, args...)

	cmd := newCmd(t.Context(), appDir, args...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, out)
}

// Run runs the application and returns the output.
// It waits for the application to complete.
func Run(t *testing.T, dir string, args ...string) string {
	appName := "./" + filepath.Base(dir)
	cmd := newCmd(t.Context(), dir, append([]string{appName}, args...)...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, out)
	return string(out)
}

// Start starts the application but does not wait for it to complete.
// It returns the command and the combined output pipe(stdout and stderr).
func Start(t *testing.T, dir string, args ...string) (*exec.Cmd, io.ReadCloser) {
	appName := "./" + filepath.Base(dir)
	cmd := newCmd(t.Context(), dir, append([]string{appName}, args...)...)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	cmd.Stderr = cmd.Stdout // redirect stderr to stdout for easier debugging
	err = cmd.Start()
	require.NoError(t, err)
	t.Cleanup(func() {
		if cmd.Process != nil && cmd.ProcessState == nil {
			require.NoError(t, cmd.Process.Kill())
		}
	})
	return cmd, stdout
}
