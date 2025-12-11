// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/app"
)

func TestHTTPClientIntegration(t *testing.T) {
	clientDir := filepath.Join("..", "..", "demo", "http", "client")
	serverDir := filepath.Join("..", "..", "demo", "http", "server")

	// Enable debug logging
	t.Setenv("OTEL_LOG_LEVEL", "debug")

	// Build both client and server with instrumentation
	t.Log("Building instrumented HTTP client and server...")
	app.Build(t, clientDir, "go", "build", "-a")
	app.Build(t, serverDir, "go", "build", "-a")

	// Start the server
	t.Log("Starting HTTP server...")
	serverCmd, outputPipe := app.Start(t, serverDir, "-port=8083", "-no-faults", "-no-latency")
	waitUntilDone, err := app.WaitForServerReady(t, serverCmd, outputPipe)
	require.NoError(t, err, "server should start successfully")

	// Run the client (makes request and exits)
	t.Log("Running HTTP client...")
	clientOutput := app.Run(t, clientDir, "-addr=http://localhost:8083", "-count=1")

	// Shutdown the server
	t.Log("Shutting down server...")
	app.Run(t, clientDir, "-addr=http://localhost:8083", "-shutdown")

	// Wait for server to exit and get output
	serverOutput := waitUntilDone()

	// Verify client instrumentation
	t.Log("Verifying client instrumentation...")
	require.Contains(t, clientOutput,
		"HTTP client instrumentation initialized", "client should initialize instrumentation")
	require.Contains(t, clientOutput, "BeforeRoundTrip called", "client before hook should be called")
	require.Contains(t, clientOutput, "AfterRoundTrip called", "client after hook should be called")

	// Verify server instrumentation
	t.Log("Verifying server instrumentation...")
	require.Contains(t, serverOutput,
		"HTTP server instrumentation initialized", "server should initialize instrumentation")
	require.Contains(t, serverOutput, "BeforeServeHTTP called", "server before hook should be called")
	require.Contains(t, serverOutput, "AfterServeHTTP called", "server after hook should be called")

	t.Log("HTTP client integration test passed!")
}
