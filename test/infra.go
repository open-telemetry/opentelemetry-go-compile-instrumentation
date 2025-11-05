// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
	"github.com/stretchr/testify/require"
)

// -----------------------------------------------------------------------------
// E2E Test Infrastructure
// This infrastructure is used to actually build the application, execute the
// compiled binary program, and verify that the output of the binary program
// is as expected.

func runCmd(ctx context.Context, dir string, args ...string) (string, error) {
	path := args[0]
	args = args[1:]
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), err
	}
	return string(output), nil
}

// BuildApp builds the application with the instrumentation tool.
func BuildApp(t *testing.T, appDir string, args ...string) {
	binName := "otel"
	if util.IsWindows() {
		binName += ".exe"
	}
	otelPath := filepath.Join("..", "..", binName)
	args = append([]string{otelPath}, args...)
	out, err := runCmd(t.Context(), appDir, args...)
	require.NoError(t, err, out)
}

// RunApp runs the application and returns the output.
func RunApp(t *testing.T, dir string) string {
	out, err := runCmd(t.Context(), dir, "./"+filepath.Base(dir))
	require.NoError(t, err, out)
	return out
}
