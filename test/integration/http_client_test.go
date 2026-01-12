// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/app"
)

// TestHTTPClientInstrumentation tests HTTP client instrumentation in isolation.
// Uses a non-instrumented httptest.Server as the target.
// Expects: 1 trace with 1 client span.
func TestHTTPClientInstrumentation(t *testing.T) {
	f := app.NewE2EFixture(t)

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"message":"Hello from test server"}`)
	}))
	defer testServer.Close()

	f.Build("http/client")

	f.RunClient("http/client", "-addr="+testServer.URL, "-count=1")

	span := f.RequireSingleSpan()
	app.RequireHTTPClientSemconv(t, span, "GET", testServer.URL+"/greet?name=world", "127.0.0.1", 200)
}
