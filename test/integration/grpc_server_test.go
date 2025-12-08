// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"bufio"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/app"
)

// TestGRPCServerIntegration tests gRPC server instrumentation
func TestGRPCServerIntegration(t *testing.T) {
	serverDir := filepath.Join("..", "..", "demo", "grpc", "server")
	clientDir := filepath.Join("..", "..", "demo", "grpc", "client")

	// Enable debug logging for instrumentation
	t.Setenv("OTEL_LOG_LEVEL", "debug")

	t.Log("Building instrumented gRPC server...")

	// Build the server application with the instrumentation tool
	app.Build(t, serverDir, "go", "build", "-a")
	app.Build(t, clientDir, "go", "build", "-a")

	t.Log("Starting gRPC server...")

	// Start the server and wait for it to be ready
	serverApp, outputPipe := app.Start(t, serverDir)
	defer func() {
		if serverApp.Process != nil {
			_ = serverApp.Process.Kill()
		}
	}()

	serverOutput := strings.Builder{}
	readyChan := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(outputPipe)
		for scanner.Scan() {
			line := scanner.Text()
			serverOutput.WriteString(line + "\n")
			if strings.Contains(line, "server started") {
				close(readyChan)
			}
		}
	}()

	// Wait for server to be ready
	select {
	case <-readyChan:
		t.Log("Server is ready!")
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server to be ready")
	}

	t.Log("Making gRPC unary request...")

	// Run unary RPC
	unaryOutput := app.Run(t, clientDir, "-name", "TestUser")
	require.Contains(t, unaryOutput, `"msg":"greeting"`, "Expected greeting response")
	require.Contains(t, unaryOutput, `"message":"Hello TestUser"`, "Expected personalized greeting")

	t.Log("Verifying server received request...")

	// Verify server processed the request
	output := serverOutput.String()
	require.Contains(t, output, `"msg":"received request"`, "Expected server to log received request")
	require.Contains(t, output, `"name":"TestUser"`, "Expected server to log request name")

	t.Log("Verifying instrumentation output...")

	// Verify instrumentation hooks were called
	require.Contains(t, output, "gRPC server instrumentation initialized", "instrumentation should be initialized")
	require.Contains(t, output, "BeforeNewServer called", "before hook should be called")
	require.Contains(t, output, "AfterNewServer called", "after hook should be called")

	t.Log("Shutting down server...")

	// Send shutdown
	app.Run(t, clientDir, "-shutdown")

	// Wait for server to exit
	_ = serverApp.Wait()

	t.Log("gRPC server integration test passed!")
}

// TestGRPCServerStreaming tests gRPC server streaming instrumentation
func TestGRPCServerStreaming(t *testing.T) {
	serverDir := filepath.Join("..", "..", "demo", "grpc", "server")
	clientDir := filepath.Join("..", "..", "demo", "grpc", "client")

	// Enable debug logging for instrumentation
	t.Setenv("OTEL_LOG_LEVEL", "debug")

	t.Log("Building instrumented gRPC server...")

	// Build the applications
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

	serverOutput := strings.Builder{}
	readyChan := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(outputPipe)
		for scanner.Scan() {
			line := scanner.Text()
			serverOutput.WriteString(line + "\n")
			if strings.Contains(line, "server started") {
				close(readyChan)
			}
		}
	}()

	// Wait for server to be ready
	select {
	case <-readyChan:
		t.Log("Server is ready!")
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server to be ready")
	}

	t.Log("Making gRPC streaming request...")

	// Run streaming RPC
	streamOutput := app.Run(t, clientDir, "-stream")
	require.Contains(t, streamOutput, "stream response", "Expected stream responses")

	t.Log("Verifying instrumentation output...")

	// Verify instrumentation hooks were called
	output := serverOutput.String()
	require.Contains(t, output, "gRPC server instrumentation initialized", "instrumentation should be initialized")
	require.Contains(t, output, "BeforeNewServer called", "before hook should be called")
	require.Contains(t, output, "AfterNewServer called", "after hook should be called")

	t.Log("Shutting down server...")

	// Send shutdown
	app.Run(t, clientDir, "-shutdown")

	// Wait for server to exit
	_ = serverApp.Wait()

	t.Log("gRPC server streaming test passed!")
}
