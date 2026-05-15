// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
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

func newCmd(ctx context.Context, dir string, args ...string) *exec.Cmd {
	path := args[0]
	args = args[1:]
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	return cmd
}

type sharedBuild struct {
	once sync.Once
	err  error
}

var sharedBuilds sync.Map

func appBinaryName() string {
	name := appBinName
	if util.IsWindows() {
		name += ".exe"
	}
	return name
}

func appBinaryPath(appDir string) string {
	return filepath.Join(appDir, appBinaryName())
}

func buildApp(ctx context.Context, appDir string, args ...string) error {
	binName := otelcBinName
	if util.IsWindows() {
		binName += ".exe"
	}
	pwd, err := os.Getwd()
	if err != nil {
		return err
	}
	otelcPath := filepath.Join(pwd, "..", "..", binName)

	args = append(args, "-o", appBinaryName())
	args = append([]string{otelcPath}, args...)

	cmd := newCmd(ctx, appDir, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build failed: %w: %s", err, string(out))
	}
	return nil
}

// Build builds the application with the instrumentation tool.
func Build(t *testing.T, appDir string, args ...string) {
	if err := buildApp(t.Context(), appDir, args...); err != nil {
		require.NoError(t, err)
	}
	t.Cleanup(func() {
		_ = os.Remove(appBinaryPath(appDir))
	})
}

// BuildShared builds the app once per process and keeps the binary for reuse.
func BuildShared(t *testing.T, appDir string, args ...string) {
	t.Helper()
	entry, _ := sharedBuilds.LoadOrStore(appDir, &sharedBuild{})
	build := entry.(*sharedBuild)
	build.once.Do(func() {
		build.err = buildApp(t.Context(), appDir, args...)
	})
	require.NoError(t, build.err)
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
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})
}
