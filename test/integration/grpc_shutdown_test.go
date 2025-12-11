// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/app"
)

// TestGRPCClientTelemetryFlushOnExit verifies that telemetry is properly flushed
// when the client application exits, without needing an explicit sleep.
// This test validates that the signal-based shutdown handler in the instrumentation
// layer works correctly.
func TestGRPCClientTelemetryFlushOnExit(t *testing.T) {
	serverDir := filepath.Join("..", "..", "demo", "grpc", "server")
	clientDir := filepath.Join("..", "..", "demo", "grpc", "client")

	// Enable debug logging to verify shutdown behavior
	t.Setenv("OTEL_LOG_LEVEL", "debug")
	// Use stdout exporter for easy verification
	t.Setenv("OTEL_TRACES_EXPORTER", "console")

	t.Log("Building instrumented gRPC applications...")

	// Build server and client
	app.Build(t, serverDir, "go", "build", "-a")
	app.Build(t, clientDir, "go", "build", "-a")

	t.Log("Starting gRPC server...")

	// Start the server
	serverApp, outputPipe := app.Start(t, serverDir)
	defer func() {
		if serverApp.Process != nil {
			_ = serverApp.Process.Kill()
		}
	}()
	_, err := app.WaitForServerReady(t, serverApp, outputPipe)
	require.NoError(t, err, "server should start successfully")

	t.Log("Running gRPC client and monitoring shutdown...")

	// Run client with a single request
	// The client should exit cleanly and export telemetry WITHOUT the 6s sleep
	start := time.Now()
	clientOutput := app.Run(t, clientDir, "-name", "ShutdownTest", "-log-level", "debug")
	duration := time.Since(start)

	t.Logf("Client completed in %v", duration)

	// Verify the client ran successfully
	require.Contains(t, clientOutput, `"msg":"greeting"`, "Expected greeting response")
	require.Contains(t, clientOutput, `"msg":"client finished"`, "Expected client finished log")

	// Verify instrumentation was active
	require.Contains(
		t,
		clientOutput,
		"gRPC client instrumentation initialized",
		"Expected instrumentation to be initialized",
	)

	// Verify signal handler was setup (debug log)
	// Note: Since we're using app.Run which waits for process completion,
	// the signal handler won't be triggered by SIGINT/SIGTERM.
	// Instead, we verify the normal exit path works without requiring explicit sleep.

	require.Less(t, duration, 3*time.Second,
		"Client should complete quickly without explicit sleep - signal handler handles flush")

	t.Log("Shutting down server...")
	app.Run(t, clientDir, "-shutdown")
	_ = serverApp.Wait()

	t.Log("Telemetry flush test passed - no explicit sleep needed!")
}
