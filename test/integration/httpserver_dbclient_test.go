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

func TestHTTPServerDBClient(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "httpserverdbclient", "go", "build", "-a")

	f := testutil.NewTestFixture(t)
	frontPort := testutil.FreePort(t)

	f.Start("httpserverdbclient", fmt.Sprintf("-front-port=%d", frontPort))
	testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", frontPort))

	// Send request to frontend
	url := fmt.Sprintf("http://127.0.0.1:%d/hello", frontPort)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Wait for the spans to be flushed
	testutil.WaitForSpanFlush(t)

	// We expect exactly 1 trace with 2 spans:
	// 1. HTTP server (Frontend)
	// 2. SQL client (Frontend -> Database)
	f.RequireTraceCount(1)
	f.RequireSpansPerTrace(2)

	httpServerSpan := testutil.RequireSpan(
		t,
		f.Traces(),
		testutil.IsServer,
		func(s ptrace.Span) bool { return s.Name() == "GET" },
	)
	sqlClientSpan := testutil.RequireSpan(t, f.Traces(), testutil.IsClient)

	// Assert on propagation (parent-child relationships)
	require.Equal(t, httpServerSpan.TraceID(), sqlClientSpan.TraceID(), "trace ID mismatch")
	require.Equal(t, httpServerSpan.SpanID(), sqlClientSpan.ParentSpanID(), "SQL client parent must be HTTP server")
}
