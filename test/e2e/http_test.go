//go:build e2e

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"testing"

	"github.com/stretchr/testify/require"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/app"
)

func TestHttp(t *testing.T) {
	// 1. Setup fixture (starts collector + configures OTEL env vars)
	f := app.NewE2EFixture(t)

	// 2. Build the server and client applications with the instrumentation tool
	f.Build("http/server")
	f.Build("http/client")

	// 3. Start the server and wait for it to be ready
	server := f.StartServer("http/server", "-no-faults", "-no-latency")

	// 4. Send requests to generate traces
	f.RunClient("http/client", "-name", "test")

	// 5. Shutdown the server
	f.RunClient("http/client", "-shutdown")

	// 6. Stop server and verify instrumentation was initialized
	output := server.Stop()
	require.Contains(t, output, "HTTP server instrumentation initialized")

	// 7. Verify trace counts
	f.RequireTraceCount(2)    // greet + shutdown requests
	f.RequireSpansPerTrace(2) // client + server per trace

	// 8. Verify HTTP client span semantic conventions
	greetClientSpan := app.RequireSpan(t, f.Traces(),
		app.IsClient,
		app.HasAttributeContaining(string(semconv.URLFullKey), "/greet"),
	)
	app.RequireHTTPClientSemconv(t, greetClientSpan, "GET", "http://localhost:8080/greet?name=test", "localhost", 200)

	// 9. Verify HTTP server span semantic conventions
	greetServerSpan := app.RequireSpan(t, f.Traces(),
		app.IsServer,
		app.HasAttribute(string(semconv.URLPathKey), "/greet"),
	)
	app.RequireHTTPServerSemconv(t, greetServerSpan, "GET", "/greet", "http", 200)
}
