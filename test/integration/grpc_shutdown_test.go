// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/testutil"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

// TestGRPCServerTelemetryFlushOnSignal verifies that telemetry is properly flushed
// when the server receives SIGINT, using the batch span processor.
// This test validates that the signal-based shutdown handler in the instrumentation
// layer correctly triggers a flush before exit.
func TestGRPCServerTelemetryFlushOnSignal(t *testing.T) {
	f := testutil.NewTestFixture(t)
	t.Setenv("OTEL_GO_SIMPLE_SPAN_PROCESSOR", "false")

	f.Build("grpcserver")
	cmd := startServerProcess(t, "-port=50052")
	time.Sleep(time.Second)

	client := NewGRPCClient(t, "localhost:50052")
	client.SayHello(t, "ShutdownTest")

	require.NoError(t, cmd.Process.Signal(os.Interrupt))
	waitForProcessExit(t, cmd, 10*time.Second)
	time.Sleep(500 * time.Millisecond)

	spans := testutil.AllSpans(f.Traces())
	require.NotEmpty(t, spans, "expected spans to be flushed on SIGINT shutdown")

	serverSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsServer,
		testutil.HasAttribute(string(semconv.RPCSystemKey), "grpc"),
	)
	testutil.RequireGRPCServerSemconv(t, serverSpan, "greeter.Greeter", "SayHello", 0)
}

// startServerProcess starts the instrumented grpcserver app and returns the exec.Cmd.
func startServerProcess(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()

	pwd, err := os.Getwd()
	require.NoError(t, err)
	appDir := filepath.Join(pwd, "..", "apps", "grpcserver")

	appName := "./app"
	if util.IsWindows() {
		appName += ".exe"
	}

	cmd := exec.CommandContext(t.Context(), appName, args...)
	cmd.Dir = appDir
	cmd.Env = os.Environ()

	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})

	return cmd
}

// waitForProcessExit waits for a process to exit within the given timeout.
func waitForProcessExit(t *testing.T, cmd *exec.Cmd, timeout time.Duration) {
	t.Helper()

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatal("process did not exit within timeout")
	}
}
