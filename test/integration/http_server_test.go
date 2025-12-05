// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"bufio"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/app"
)

func waitForServerReady(t *testing.T, serverCmd *exec.Cmd, output io.ReadCloser) func() string {
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
		t.Logf("Server is ready!")
	case <-time.After(15 * time.Second):
		t.Fatal("timeout waiting for server to be ready")
	}

	return func() string {
		serverCmd.Wait()
		<-doneChan
		return outputBuilder.String()
	}
}

func TestHTTPServerIntegration(t *testing.T) {
	serverDir := filepath.Join("..", "..", "demo", "http", "server")

	// Enable debug logging for instrumentation
	t.Setenv("OTEL_LOG_LEVEL", "debug")

	// Build the server with instrumentation
	t.Log("Building instrumented HTTP server...")
	app.Build(t, serverDir, "go", "build", "-a")

	// Start the server
	t.Log("Starting HTTP server...")
	serverCmd, outputPipe := app.Start(t, serverDir, "-port=8081", "-no-faults", "-no-latency")
	waitUntilDone := waitForServerReady(t, serverCmd, outputPipe)

	// Give server a moment to fully initialize
	time.Sleep(500 * time.Millisecond)

	// Make a test request
	t.Log("Making test GET request...")
	resp, err := http.Get("http://localhost:8081/greet?name=integration-test")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// Make a POST request
	t.Log("Making test POST request...")
	resp2, err := http.Post("http://localhost:8081/greet", "application/json", strings.NewReader(`{"name":"test"}`))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp2.StatusCode)
	resp2.Body.Close()

	// Shutdown the server
	t.Log("Shutting down server...")
	resp3, err := http.Get("http://localhost:8081/shutdown")
	if err == nil {
		resp3.Body.Close()
	}

	// Wait for server to exit and get output
	output := waitUntilDone()

	// Verify instrumentation hooks were called
	t.Log("Verifying instrumentation output...")
	require.Contains(t, output, "HTTP server instrumentation initialized", "instrumentation should be initialized")
	require.Contains(t, output, "BeforeServeHTTP called", "before hook should be called")
	require.Contains(t, output, "AfterServeHTTP called", "after hook should be called")
	require.Contains(t, output, "method\":\"GET", "should log GET request")
	require.Contains(t, output, "status_code\":200", "should capture status code")

	t.Log("HTTP server integration test passed!")
}

func TestHTTPServerInstrumentationDisabled(t *testing.T) {
	serverDir := filepath.Join("..", "..", "demo", "http", "server")

	// Enable debug logging and disable nethttp instrumentation
	t.Setenv("OTEL_LOG_LEVEL", "debug")
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "nethttp")

	// Build the server with instrumentation
	t.Log("Building instrumented HTTP server...")
	app.Build(t, serverDir, "go", "build", "-a")

	// Start server with instrumentation disabled
	t.Log("Starting HTTP server with instrumentation disabled...")

	serverCmd, outputPipe := app.Start(t, serverDir, "-port=8082", "-no-faults", "-no-latency")
	waitUntilDone := waitForServerReady(t, serverCmd, outputPipe)

	time.Sleep(500 * time.Millisecond)

	// Make a test request
	t.Log("Making test request...")
	resp, err := http.Get("http://localhost:8082/greet?name=test")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// Shutdown
	resp2, err := http.Get("http://localhost:8082/shutdown")
	if err == nil {
		resp2.Body.Close()
	}

	output := waitUntilDone()

	// Verify instrumentation was disabled
	require.Contains(t, output, "HTTP server instrumentation disabled", "instrumentation should be disabled")
	require.NotContains(t, output, "BeforeServeHTTP called", "before hook should not execute logic when disabled")

	t.Log("HTTP server disabled test passed!")
}
