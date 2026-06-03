// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

const (
	otelcBinName = "otelc"
	appBinName   = "app"
)

// -----------------------------------------------------------------------------
// E2E Test Infrastructure
// This infrastructure is used to actually build the application with the otelc
// instrumentation tool, execute the application and verify the output.

// newCmd builds an exec.Cmd in dir with the given env. If env is nil, the
// parent process env is used.
func newCmd(ctx context.Context, dir string, env []string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	if env == nil {
		cmd.Env = os.Environ()
	} else {
		cmd.Env = env
	}
	return cmd
}

// otelcPath returns the absolute path to the otelc binary, assuming the
// caller's working directory is a sibling of the repo's otelc output.
func otelcPath() (string, error) {
	binName := otelcBinName
	if util.IsWindows() {
		binName += ".exe"
	}
	pwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(pwd, "..", "..", binName), nil
}

// appOutputName returns the platform-specific name used for built test binaries.
func appOutputName() string {
	if util.IsWindows() {
		return appBinName + ".exe"
	}
	return appBinName
}

// Build builds the application with the instrumentation tool. The built binary
// is registered for cleanup via t.Cleanup.
func Build(t *testing.T, appDir string, args ...string) {
	t.Helper()
	otelc, err := otelcPath()
	require.NoError(t, err)

	output := appOutputName()
	args = append(args, "-o", output)
	args = append([]string{otelc}, args...)

	cmd := newCmd(t.Context(), appDir, nil, args...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	t.Cleanup(func() {
		_ = os.Remove(filepath.Join(appDir, output))
	})
}

// BuildAppAt builds the app at the given directory using context ctx. Intended
// for use from TestMain where no *testing.T is available. The caller is
// responsible for cleaning up the built binary via CleanupAppAt.
func BuildAppAt(ctx context.Context, appDir string) error {
	otelc, err := otelcPath()
	if err != nil {
		return fmt.Errorf("locate otelc: %w", err)
	}

	output := appOutputName()
	args := []string{otelc, "go", "build", "-a", "-o", output}

	cmd := newCmd(ctx, appDir, nil, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("otelc build in %s: %w\n%s", appDir, err, string(out))
	}
	return nil
}

// CleanupAppAt removes the binary that BuildAppAt produced in appDir.
func CleanupAppAt(appDir string) {
	_ = os.Remove(filepath.Join(appDir, appOutputName()))
}

// Run runs the application and returns the output. It waits for the
// application to complete. If env is nil, the parent process env is used.
func Run(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	appName := "./" + appBinName
	if util.IsWindows() {
		appName += ".exe"
	}
	cmd := newCmd(t.Context(), dir, env, append([]string{appName}, args...)...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return string(out)
}

// Start starts the application but does not wait for it to complete. If env
// is nil, the parent process env is used. Stdout and stderr are captured and
// logged when the test fails, so that app crashes are visible in CI output.
func Start(t *testing.T, dir string, env []string, args ...string) {
	t.Helper()
	appName := "./" + appBinName
	if util.IsWindows() {
		appName += ".exe"
	}
	cmd := newCmd(t.Context(), dir, env, append([]string{appName}, args...)...)

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait() // ensure all output is flushed
		}
		if t.Failed() && buf.Len() > 0 {
			t.Logf("app output:\n%s", buf.String())
		}
	})
}
