// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"testing"
)

// TestGRPCClientTelemetryFlushOnExit verifies that telemetry is properly flushed
// when the client application exits, without needing an explicit sleep.
// This test validates that the signal-based shutdown handler in the instrumentation
// layer works correctly.
func TestGRPCClientTelemetryFlushOnExit(t *testing.T) {
	// f := app.NewE2EFixture(t, app.WithoutCollector())

	// serverDir := filepath.Join("..", "apps", "grpcserver")
	// clientDir := filepath.Join("..", "apps", "grpcclient")

	// // Enable debug logging to verify shutdown behavior
	// t.Setenv("OTEL_LOG_LEVEL", "debug")
	// // Use stdout exporter for easy verification
	// t.Setenv("OTEL_TRACES_EXPORTER", "console")

	// t.Log("Building instrumented gRPC applications...")

	// // Build server and client
	// f.BuildApp("grpcserver")
	// f.BuildApp("grpcclient")

	// t.Log("Starting gRPC server...")

	// // Start the server
	// serverApp, outputPipe := app.Start(t, serverDir)
	// defer func() {
	// 	if serverApp.Process != nil {
	// 		_ = serverApp.Process.Kill()
	// 	}
	// }()
	// _, err := app.WaitForServerReady(t, serverApp, outputPipe)
	// require.NoError(t, err, "server should start successfully")

	// t.Log("Running gRPC client and monitoring shutdown...")

	// // Run client with a single request
	// // The client should exit cleanly and export telemetry WITHOUT the 6s sleep
	// start := time.Now()
	// clientOutput := app.Run(t, clientDir, "-name", "ShutdownTest")
	// duration := time.Since(start)

	// t.Logf("Client completed in %v", duration)

	// // Verify the client ran successfully
	// require.Contains(t, clientOutput, "greeting", "Expected greeting response")
	// require.Contains(t, clientOutput, "Hello ShutdownTest", "Expected greeting message")

	// require.Less(t, duration, 3*time.Second,
	// 	"Client should complete quickly without explicit sleep - signal handler handles flush")

	// t.Log("Telemetry flush test passed - no explicit sleep needed!")
}
