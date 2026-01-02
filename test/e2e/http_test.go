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

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/ptrace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/app"
)

func waitUntilReady(t *testing.T, serverApp *exec.Cmd, outputPipe io.ReadCloser) func() string {
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
		t.Logf("Server is ready!")
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for server to be ready")
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

func TestHttp(t *testing.T) {
	serverDir := filepath.Join("..", "..", "demo", "http", "server")
	clientDir := filepath.Join("..", "..", "demo", "http", "client")

	collector := app.StartCollector(t)
	defer collector.Close()

	t.Setenv("OTEL_SERVICE_NAME", "test-service")
	t.Setenv("OTEL_TRACES_EXPORTER", "otlp")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", collector.URL)
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")

	// Build the server and client applications with the instrumentation tool.
	app.Build(t, serverDir, "go", "build", "-a")
	app.Build(t, clientDir, "go", "build", "-a")

	// Start the server and wait for it to be ready.
	// Disable fault injection and latency for predictable test results.
	serverApp, outputPipe := app.Start(t, serverDir, "-no-faults", "-no-latency")
	waitUntilDone := waitUntilReady(t, serverApp, outputPipe)

	// Send a regular request first to generate traces
	app.Run(t, clientDir, "-name", "test")

	// Now send shutdown request to the server
	app.Run(t, clientDir, "-shutdown")

	// Wait for the server to exit and return the output.
	output := waitUntilDone()

	// Verify that server instrumentation was initialized
	require.Contains(t, output, "HTTP server instrumentation initialized")

	stats := app.AnalyzeTraces(t, collector.Traces)

	require.Equal(t, 2, stats.TraceCount, "Expected 2 traces (greet + shutdown requests)")
	for traceID, count := range stats.SpansPerTrace {
		require.Equal(t, 2, count, "Trace %s should have 2 spans (client + server)", traceID[:16])
	}

	greetClientSpan := app.RequireSpan(t, collector.Traces,
		app.IsClient,
		app.HasAttributeContaining(string(semconv.URLFullKey), "/greet"),
	)
	requireHTTPClientSemconv(t, greetClientSpan, "GET", 200)

	greetServerSpan := app.RequireSpan(t, collector.Traces,
		app.IsServer,
		app.HasAttribute(string(semconv.URLPathKey), "/greet"),
	)
	requireHTTPServerSemconv(t, greetServerSpan, "GET", 200)
}

// Reference: https://opentelemetry.io/docs/specs/semconv/http/http-spans/#http-client-span
func requireHTTPClientSemconv(t *testing.T, span ptrace.Span, method string, statusCode int64) {
	// Required attributes
	app.RequireAttribute(t, span, string(semconv.HTTPRequestMethodKey), method)
	app.RequireAttributeExists(t, span, string(semconv.URLFullKey))
	app.RequireAttributeExists(t, span, string(semconv.ServerAddressKey))
	// Conditionally required (when response is received)
	app.RequireAttribute(t, span, string(semconv.HTTPResponseStatusCodeKey), statusCode)
	// Recommended attributes
	app.RequireAttributeExists(t, span, string(semconv.NetworkProtocolVersionKey))
	app.RequireAttributeExists(t, span, string(semconv.URLSchemeKey))
	app.RequireAttributeExists(t, span, string(semconv.ServerPortKey))
}

// Reference: https://opentelemetry.io/docs/specs/semconv/http/http-spans/#http-server-span
func requireHTTPServerSemconv(t *testing.T, span ptrace.Span, method string, statusCode int64) {
	// Required attributes
	app.RequireAttribute(t, span, string(semconv.HTTPRequestMethodKey), method)
	app.RequireAttributeExists(t, span, string(semconv.URLPathKey))
	app.RequireAttributeExists(t, span, string(semconv.URLSchemeKey))
	// Conditionally required (when response is sent)
	app.RequireAttribute(t, span, string(semconv.HTTPResponseStatusCodeKey), statusCode)
	// Recommended attributes
	app.RequireAttributeExists(t, span, string(semconv.ClientAddressKey))
	app.RequireAttributeExists(t, span, string(semconv.UserAgentOriginalKey))
	app.RequireAttributeExists(t, span, string(semconv.NetworkProtocolVersionKey))
	app.RequireAttributeExists(t, span, string(semconv.ServerAddressKey))
	app.RequireAttributeExists(t, span, string(semconv.ServerPortKey))
}
