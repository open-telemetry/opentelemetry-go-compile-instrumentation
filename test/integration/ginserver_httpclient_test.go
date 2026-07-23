// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"go.opentelemetry.io/otelc/test/testutil"
)

func TestGinServerHTTPClient(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "ginserverhttpclient", "go", "build", "-a")

	f := testutil.NewTestFixture(t)
	frontPort := testutil.FreePort(t)
	backPort := testutil.FreePort(t)

	f.Start("ginserverhttpclient", fmt.Sprintf("-front-port=%d", frontPort), fmt.Sprintf("-back-port=%d", backPort))
	testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", frontPort))
	testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", backPort))

	// Send request to frontend
	url := fmt.Sprintf("http://127.0.0.1:%d/hello", frontPort)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Wait for the spans from the frontend, client, and backend to be flushed
	testutil.WaitForSpanFlush(t)

	// We expect exactly 1 trace with 3 spans:
	// 1. Gin server (Frontend)
	// 2. HTTP client (Frontend -> Backend)
	// 3. HTTP server (Backend)
	f.RequireTraceCount(1)
	f.RequireSpansPerTrace(3)

	ginServerSpan := testutil.RequireSpan(t, f.Traces(), testutil.IsServer, func(s ptrace.Span) bool { return s.Name() == "GET /hello" })
	httpClientSpan := testutil.RequireSpan(t, f.Traces(), testutil.IsClient)
	backendServerSpan := testutil.RequireSpan(t, f.Traces(), testutil.IsServer, func(s ptrace.Span) bool { return s.Name() == "GET" })

	// Assert on propagation (parent-child relationships)
	require.Equal(t, ginServerSpan.TraceID(), httpClientSpan.TraceID(), "trace ID mismatch")
	require.Equal(t, ginServerSpan.TraceID(), backendServerSpan.TraceID(), "trace ID mismatch")

	require.Equal(t, ginServerSpan.SpanID(), httpClientSpan.ParentSpanID(), "HTTP client parent must be Gin server")
	require.Equal(t, httpClientSpan.SpanID(), backendServerSpan.ParentSpanID(), "Backend server parent must be HTTP client")
}
