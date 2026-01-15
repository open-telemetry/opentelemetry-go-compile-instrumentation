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

func TestHTTPClient(t *testing.T) {
	testCases := []struct {
		name       string
		queryParam string
	}{
		{
			name:       "basic",
			queryParam: "world",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f := app.NewE2EFixture(t)
			server := StartHTTPServerWithResponse(t, 200, `{"message":"Hello"}`)

			f.BuildApp("httpclient")
			f.RunApp("httpclient", "-addr="+server.URL, "-name="+tc.queryParam)

			span := f.RequireSingleSpan()
			expectedURL := server.URL + "/hello?name=" + tc.queryParam
			app.RequireHTTPClientSemconv(t, span, "GET", expectedURL, "127.0.0.1", 200)
		})
	}
}

// HTTPServer wraps a test HTTP server.
type HTTPServer struct {
	*httptest.Server
}

// StartHTTPServer creates and starts a test HTTP server with a custom handler.
// The server is automatically closed when the test completes.
func StartHTTPServer(t *testing.T, handler http.Handler) *HTTPServer {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return &HTTPServer{Server: server}
}

// StartHTTPServerWithResponse creates a test HTTP server that returns the given status and body.
func StartHTTPServerWithResponse(t *testing.T, status int, body string) *HTTPServer {
	t.Helper()

	return StartHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		fmt.Fprintln(w, body)
	}))
}
