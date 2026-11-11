// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/util"
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

// OtelcPath returns the absolute path to the otelc binary, assuming the
// caller's working directory is a sibling of the repo's otelc output.
func OtelcPath() (string, error) {
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

// appsPath returns the path to the test apps directory.
func appsPath() (string, error) {
	pwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(pwd, "..", "apps"), nil
}

// Build builds the application with the instrumentation tool. The built binary
// is registered for cleanup via t.Cleanup. Standard test apps use
// OTELC_TEST_GOCACHE/<app> as GOCACHE when OTELC_TEST_GOCACHE is set, and drop
// -a so warm builds can reuse that per-app cache. Custom appsDir builds keep
// the caller's arguments and environment unchanged.
func Build(t *testing.T, appsDir, app string, args ...string) {
	t.Helper()
	otelc, err := OtelcPath()
	require.NoError(t, err)

	standardAppsDir := appsDir == ""
	var env []string
	if standardAppsDir {
		cacheRoot := os.Getenv("OTELC_TEST_GOCACHE")
		if cacheRoot != "" {
			cacheDir := filepath.Join(cacheRoot, app)
			require.NoError(t, os.MkdirAll(cacheDir, 0o755))
			env = append(os.Environ(), "GOCACHE="+cacheDir)

			// Dropping -a lets warm builds reuse this app-local cache without sharing
			// compiled objects across different instrumentation configs.
			filteredArgs := make([]string, 0, len(args))
			for _, arg := range args {
				if arg != "-a" {
					filteredArgs = append(filteredArgs, arg)
				}
			}
			args = filteredArgs
		}
	}

	output := appOutputName()
	args = append(args, "-o", output)
	args = append([]string{otelc}, args...)

	if standardAppsDir {
		var err error
		appsDir, err = appsPath()
		require.NoError(t, err)
	}
	appDir := filepath.Join(appsDir, app)

	cmd := newCmd(t.Context(), appDir, env, args...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	t.Cleanup(func() {
		_ = os.Remove(filepath.Join(appDir, output))
		_ = os.RemoveAll(filepath.Join(appDir, ".otelc-build"))
		// Left behind only when the otelc subprocess was killed mid-run
		// (release removes it on every normal exit).
		_ = os.Remove(filepath.Join(appDir, ".otelc-build.lock"))
	})
}

// Run runs the application and returns the output. It waits for the
// application to complete. If env is nil, the parent process env is used.
func Run(t *testing.T, appsDir, app string, env []string, args ...string) string {
	t.Helper()
	appName := "./" + appBinName
	if util.IsWindows() {
		appName += ".exe"
	}
	if appsDir == "" {
		var err error
		appsDir, err = appsPath()
		require.NoError(t, err)
	}
	appDir := filepath.Join(appsDir, app)
	cmd := newCmd(t.Context(), appDir, env, append([]string{appName}, args...)...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return string(out)
}

// hasStdinShutdown reports whether -stdin-shutdown is specified in args.
func hasStdinShutdown(args []string) bool {
	for _, arg := range args {
		if arg == "-stdin-shutdown" {
			return true
		}
	}
	return false
}

// Start starts the application but does not wait for it to complete. If env
// is nil, the parent process env is used. Stdout and stderr are captured and
// logged when the test fails, so that app crashes are visible in CI output.
// The returned Server manages the process lifecycle and provides graceful shutdown via Stop.
// Note: Cmd.Wait() is managed by Server; tests must not invoke Cmd.Wait() directly.
func Start(t *testing.T, appsDir, app string, env []string, args ...string) *Server {
	t.Helper()
	appName := "./" + appBinName
	if util.IsWindows() {
		appName += ".exe"
	}
	if appsDir == "" {
		var err error
		appsDir, err = appsPath()
		require.NoError(t, err)
	}
	appDir := filepath.Join(appsDir, app)
	cmd := newCmd(t.Context(), appDir, env, append([]string{appName}, args...)...)

	// An open stdin pipe is created only when -stdin-shutdown is explicitly requested,
	// leaving default stdin behavior (EOF) intact for all other test applications.
	var stdin io.WriteCloser
	if hasStdinShutdown(args) {
		var err error
		stdin, err = cmd.StdinPipe()
		require.NoError(t, err)
	}

	out := &AppOutput{}
	cmd.Stdout = out
	cmd.Stderr = out

	require.NoError(t, cmd.Start())

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	srv := &Server{
		t:       t,
		appPath: app,
		output:  out,
		stdin:   stdin,
		Cmd:     cmd,
		done:    done,
	}
	t.Cleanup(func() {
		srv.cleanup()
	})

	return srv
}
