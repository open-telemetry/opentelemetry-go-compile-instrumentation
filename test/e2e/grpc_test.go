//go:build e2e

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"bufio"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/app"
	"github.com/stretchr/testify/require"
)

func waitUntilGrpcReady(t *testing.T, serverApp *exec.Cmd, outputPipe io.ReadCloser) func() string {
	t.Helper()

	readyChan := make(chan struct{})
	doneChan := make(chan struct{})
	output := strings.Builder{}
	const readyMsg = "server started"
	go func() {
		// Scan will return false when the application exits.
		defer close(doneChan)
		scanner := bufio.NewScanner(outputPipe)
		for scanner.Scan() {
			line := scanner.Text()
			output.WriteString(line + "\n")
			if strings.Contains(line, readyMsg) {
				close(readyChan)
			}
		}
	}()

	select {
	case <-readyChan:
		t.Logf("gRPC Server is ready!")
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for gRPC server to be ready")
	}

	return func() string {
		// Wait for the server to exit
		serverApp.Wait()
		// Wait for the output goroutine to finish
		<-doneChan
		// Return the complete output
		return output.String()
	}
}

func TestGrpc(t *testing.T) {
	serverDir := filepath.Join("..", "..", "demo", "grpc", "server")
	clientDir := filepath.Join("..", "..", "demo", "grpc", "client")

	// Build the server and client applications with the instrumentation tool.
	app.Build(t, serverDir, "go", "build", "-a")
	app.Build(t, clientDir, "go", "build", "-a")

	// Start the server and wait for it to be ready.
	serverApp, outputPipe := app.Start(t, serverDir)
	waitUntilDone := waitUntilGrpcReady(t, serverApp, outputPipe)

	// Run the client to make a unary RPC call
	app.Run(t, clientDir, "-name", "OpenTelemetry")

	// Run the client again for streaming RPC
	app.Run(t, clientDir, "-stream")

	// Finally, send shutdown request to the server
	app.Run(t, clientDir, "-shutdown")

	// Wait for the server to exit and return the output.
	output := waitUntilDone()

	// Verify that the instrumentation was initialized
	require.Contains(t, output, "gRPC server instrumentation initialized", "instrumentation should be initialized")

	// Verify that the server started (JSON format)
	require.Contains(t, output, `"msg":"server listening"`)

	// The output should show that the gRPC server received requests (JSON format)
	require.Contains(t, output, `"msg":"received request"`)
}
