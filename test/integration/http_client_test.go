// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/testutil"
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
			f := testutil.NewTestFixture(t)
			server := StartHTTPServerWithResponse(t, 200, `{"message":"Hello"}`)

			f.BuildAndRun("httpclient", "-addr="+server.URL, "-name="+tc.queryParam)

			span := f.RequireSingleSpan()
			expectedURL := server.URL + "/hello?name=" + tc.queryParam
			testutil.RequireHTTPClientSemconv(t, span, "GET", expectedURL, "127.0.0.1", 200, server.Port(), "1.1", "http")
		})
	}
}

// HTTPServer wraps a test HTTP server.
type HTTPServer struct {
	t *testing.T
	*httptest.Server
}

func (s *HTTPServer) Port() int64 {
	u, err := url.Parse(s.URL)
	require.NoError(s.t, err)
	port, err := strconv.ParseInt(u.Port(), 10, 64)
	require.NoError(s.t, err)
	return port
}

// StartHTTPServer creates and starts a test HTTP server with a custom handler.
// The server is automatically closed when the test completes.
func StartHTTPServer(t *testing.T, handler http.Handler) *HTTPServer {
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return &HTTPServer{t: t, Server: server}
}

// StartHTTPServerWithResponse creates a test HTTP server that returns the given status and body.
func StartHTTPServerWithResponse(t *testing.T, status int, body string) *HTTPServer {
	return StartHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		fmt.Fprintln(w, body)
	}))
}
