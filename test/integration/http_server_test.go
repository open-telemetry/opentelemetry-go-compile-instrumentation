// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/app"
)

// TestHTTPServerInstrumentation tests HTTP server instrumentation in isolation.
// Uses non-instrumented http.Get() to hit the instrumented server.
// Expects: 1 trace with 1 server span.
func TestHTTPServerInstrumentation(t *testing.T) {
	f := app.NewE2EFixture(t)

	// Build server WITH instrumentation
	f.Build("http/server")

	// Start the instrumented server
	server := f.StartServer("http/server", "-port=8081", "-no-faults", "-no-latency")

	resp, err := http.Get("http://localhost:8081/greet?name=test")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	resp2, _ := http.Get("http://localhost:8081/shutdown")
	if resp2 != nil {
		resp2.Body.Close()
	}

	serverOutput := server.Stop()
	t.Logf("Server output:\n%s", serverOutput)

	// We expect 2 traces: one for /greet and one for /shutdown
	f.RequireTraceCount(2)

	// Find and verify the /greet span
	greetSpan := app.RequireSpan(t, f.Traces(),
		app.IsServer,
		app.HasAttribute("url.path", "/greet"),
	)
	app.RequireHTTPServerSemconv(t, greetSpan, "GET", "/greet", "http", 200)
}

// TestHTTPServerDisabled verifies no spans when instrumentation is disabled.
func TestHTTPServerDisabled(t *testing.T) {
	f := app.NewE2EFixture(t)

	// Disable nethttp instrumentation
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "nethttp")

	f.Build("http/server")
	server := f.StartServer("http/server", "-port=8082", "-no-faults", "-no-latency")

	resp, err := http.Get("http://localhost:8082/greet?name=test")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	resp2, _ := http.Get("http://localhost:8082/shutdown")
	if resp2 != nil {
		resp2.Body.Close()
	}
	server.Stop()

	stats := app.AnalyzeTraces(t, f.Traces())
	require.Equal(t, 0, stats.TraceCount, "No spans when instrumentation is disabled")
}
