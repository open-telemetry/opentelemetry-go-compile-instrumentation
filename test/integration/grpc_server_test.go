// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"bufio"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/app"
)

func waitForGrpcServerReady(t *testing.T, serverCmd *exec.Cmd, output io.ReadCloser) func() string {
	t.Helper()

	readyChan := make(chan struct{})
	doneChan := make(chan struct{})
	outputBuilder := strings.Builder{}
	const readyMsg = "server started"

	go func() {
		defer close(doneChan)
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			line := scanner.Text()
			outputBuilder.WriteString(line + "\n")
			if strings.Contains(line, readyMsg) {
				close(readyChan)
			}
		}
	}()

	select {
	case <-readyChan:
		t.Logf("gRPC Server is ready!")
	case <-time.After(15 * time.Second):
		t.Fatal("timeout waiting for gRPC server to be ready")
	}

	return func() string {
		serverCmd.Wait()
		<-doneChan
		return outputBuilder.String()
	}
}

func TestGRPCServerIntegration(t *testing.T) {
	serverDir := filepath.Join("..", "..", "demo", "grpc", "server")
	clientDir := filepath.Join("..", "..", "demo", "grpc", "client")

	// Enable debug logging for instrumentation
	t.Setenv("OTEL_LOG_LEVEL", "debug")

	// Build the server and client with instrumentation
	t.Log("Building instrumented gRPC server...")
	app.Build(t, serverDir, "go", "build", "-a")

	t.Log("Building gRPC client...")
	app.Build(t, clientDir, "go", "build", "-a")

	// Start the server
	t.Log("Starting gRPC server...")
	serverCmd, outputPipe := app.Start(t, serverDir, "-port=50051")
	waitUntilDone := waitForGrpcServerReady(t, serverCmd, outputPipe)

	// Give server a moment to fully initialize
	time.Sleep(500 * time.Millisecond)

	// Test unary RPC
	t.Log("Making unary gRPC call...")
	app.Run(t, clientDir, "-addr=localhost:50051", "-name=integration-test")

	// Test streaming RPC
	t.Log("Making streaming gRPC call...")
	app.Run(t, clientDir, "-addr=localhost:50051", "-name=stream-test", "-stream")

	// Shutdown the server
	t.Log("Shutting down gRPC server...")
	app.Run(t, clientDir, "-addr=localhost:50051", "-shutdown")

	// Get the server output
	output := waitUntilDone()

	// Verify instrumentation output
	t.Log("Verifying instrumentation output...")

	// Check that the server hook was called
	require.Contains(t, output, "[otel-grpc]", "gRPC instrumentation hook should be called")

	// Check that requests were received
	require.Contains(t, output, "Received: integration-test", "Server should have received unary request")
	require.Contains(t, output, "Received stream:", "Server should have received streaming requests")

	t.Log("gRPC server integration test passed!")
}

func TestGRPCServerInstrumentationDisabled(t *testing.T) {
	serverDir := filepath.Join("..", "..", "demo", "grpc", "server")
	clientDir := filepath.Join("..", "..", "demo", "grpc", "client")

	// Enable debug logging and disable instrumentation
	t.Setenv("OTEL_LOG_LEVEL", "debug")
	t.Setenv("OTEL_INSTRUMENTATION_GRPC_ENABLED", "false")

	// Build the server
	t.Log("Building gRPC server with instrumentation disabled...")
	app.Build(t, serverDir, "go", "build", "-a")

	t.Log("Building gRPC client...")
	app.Build(t, clientDir, "go", "build", "-a")

	// Start the server
	t.Log("Starting gRPC server with instrumentation disabled...")
	serverCmd, outputPipe := app.Start(t, serverDir, "-port=50052")
	waitUntilDone := waitForGrpcServerReady(t, serverCmd, outputPipe)

	// Give server a moment to fully initialize
	time.Sleep(500 * time.Millisecond)

	// Make a test request
	t.Log("Making test gRPC call...")
	app.Run(t, clientDir, "-addr=localhost:50052", "-name=test")

	// Shutdown the server
	t.Log("Shutting down server...")
	app.Run(t, clientDir, "-addr=localhost:50052", "-shutdown")

	// Get the output
	output := waitUntilDone()

	// Verify instrumentation was disabled
	require.Contains(t, output, "gRPC server instrumentation is disabled", "instrumentation should be disabled")
	require.NotContains(t, output, "Injecting StatsHandler", "StatsHandler should not be injected when disabled")

	// But the server should still work
	require.Contains(t, output, "Received: test", "Server should still function without instrumentation")

	t.Log("gRPC server disabled test passed!")
}
