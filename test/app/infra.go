// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

const startupTimeout = 15 * time.Second

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
	require.NoError(t, err, string(out))
}

// Run runs the application and returns the output.
// It waits for the application to complete.
func Run(t *testing.T, dir string, args ...string) string {
	appName := "./" + filepath.Base(dir)
	cmd := newCmd(t.Context(), dir, append([]string{appName}, args...)...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
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

// WaitForServerReady waits for a server to be ready by monitoring its output for "server started".
// It returns:
//   - func() string: a cleanup function that waits for the server to exit and returns its complete output
//   - error: non-nil if the server failed to start within the timeout
//
// This helper provides better error messages on timeout by including the server's output.
// Callers should check the error and use require.NoError to fail the test with proper context.
func WaitForServerReady(t *testing.T, serverCmd *exec.Cmd, output io.ReadCloser) (func() string, error) {
	t.Helper()

	readyChan := make(chan struct{})
	doneChan := make(chan struct{})
	outputBuilder := strings.Builder{}
	const readyMsg = "server started"

	// Use mutex to safely access outputBuilder from timeout handler
	var mu sync.Mutex

	go func() {
		defer close(doneChan)
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			line := scanner.Text()
			mu.Lock()
			outputBuilder.WriteString(line + "\n")
			mu.Unlock()
			if strings.Contains(line, readyMsg) {
				close(readyChan)
			}
		}
	}()

	waitUntilDone := func() string {
		_ = serverCmd.Wait()
		<-doneChan
		mu.Lock()
		defer mu.Unlock()
		return outputBuilder.String()
	}

	select {
	case <-readyChan:
		t.Logf("Server is ready!")
		return waitUntilDone, nil
	case <-time.After(startupTimeout):
		mu.Lock()
		serverOutput := outputBuilder.String()
		mu.Unlock()
		if serverOutput == "" {
			return nil, errors.New("timeout waiting for server to be ready - no server output received")
		}
		return nil, fmt.Errorf("timeout waiting for server to be ready - server output:\n%s", serverOutput)
	}
}
