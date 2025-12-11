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

// TestGRPCClientIntegration tests gRPC client instrumentation with both NewClient and DialContext
func TestGRPCClientIntegration(t *testing.T) {
	serverDir := filepath.Join("..", "..", "demo", "grpc", "server")
	clientDir := filepath.Join("..", "..", "demo", "grpc", "client")

	// Enable debug logging for instrumentation
	t.Setenv("OTEL_LOG_LEVEL", "debug")

	t.Log("Building instrumented gRPC client and server...")

	// Build the server and client applications with the instrumentation tool
	app.Build(t, serverDir, "go", "build", "-a")
	app.Build(t, clientDir, "go", "build", "-a")

	t.Log("Starting gRPC server...")

	// Start the server and wait for it to be ready
	serverApp, outputPipe := app.Start(t, serverDir)
	_, err := app.WaitForServerReady(t, serverApp, outputPipe)
	require.NoError(t, err, "server should start successfully")

	t.Log("Running gRPC client (unary RPC)...")

	// Test unary RPC - this uses grpc.NewClient internally
	clientOutput := app.Run(t, clientDir, "-name", "ClientTest")
	require.Contains(t, clientOutput, `"msg":"greeting"`, "Expected greeting response from server")
	require.Contains(t, clientOutput, `"message":"Hello ClientTest"`, "Expected personalized greeting")

	t.Log("Verifying client instrumentation...")

	// Verify client instrumentation hooks were called
	require.Contains(
		t,
		clientOutput,
		"gRPC client instrumentation initialized",
		"client instrumentation should be initialized",
	)
	require.Contains(t, clientOutput, "BeforeNewClient called", "client before hook should be called")
	require.Contains(t, clientOutput, "AfterNewClient called", "client after hook should be called")

	t.Log("Shutting down server...")

	// Send shutdown
	shutdownOutput := app.Run(t, clientDir, "-shutdown")
	require.Contains(t, shutdownOutput, `"msg":"shutdown response"`, "Expected shutdown response")

	// Wait for server to exit
	_ = serverApp.Wait()

	t.Log("gRPC client integration test passed!")
}

// TestGRPCClientServerStreaming tests gRPC streaming RPC instrumentation
func TestGRPCClientServerStreaming(t *testing.T) {
	serverDir := filepath.Join("..", "..", "demo", "grpc", "server")
	clientDir := filepath.Join("..", "..", "demo", "grpc", "client")

	// Enable debug logging for instrumentation
	t.Setenv("OTEL_LOG_LEVEL", "debug")

	t.Log("Building instrumented gRPC applications...")

	// Build the applications
	app.Build(t, serverDir, "go", "build", "-a")
	app.Build(t, clientDir, "go", "build", "-a")

	t.Log("Starting gRPC server...")

	// Start the server and wait for it to be ready
	serverApp, outputPipe := app.Start(t, serverDir)
	_, err := app.WaitForServerReady(t, serverApp, outputPipe)
	require.NoError(t, err, "server should start successfully")

	t.Log("Running gRPC client (streaming RPC)...")

	// Test streaming RPC - send 5 messages
	streamOutput := app.Run(t, clientDir, "-stream", "-count=5")
	require.Contains(t, streamOutput, `"msg":"stream response"`, "Expected stream responses")
	require.Contains(t, streamOutput, "Hello", "Expected greeting in stream response")

	t.Log("Verifying instrumentation output...")

	// Verify client instrumentation hooks were called
	require.Contains(
		t,
		streamOutput,
		"gRPC client instrumentation initialized",
		"client instrumentation should be initialized",
	)
	require.Contains(t, streamOutput, "BeforeNewClient called", "client before hook should be called")
	require.Contains(t, streamOutput, "AfterNewClient called", "client after hook should be called")

	t.Log("Shutting down server...")

	// Send shutdown
	app.Run(t, clientDir, "-shutdown")

	// Wait for server to exit
	_ = serverApp.Wait()

	t.Log("gRPC streaming integration test passed!")
}
