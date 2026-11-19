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

	f.WaitForSpans(3)

	f.RequireTraceCount(1)
	f.RequireSpansPerTrace(3)

	httpServerSpan := testutil.RequireSpan(
		t,
		f.Traces(),
		testutil.IsServer,
		func(s ptrace.Span) bool { return s.Name() == "GET /hello" },
	)
	sqlClientSpan := testutil.RequireSpan(t, f.Traces(), testutil.IsClient)
	rowsSpan := testutil.RequireSpan(t, f.Traces(), testutil.HasName("sql.rows"))
	require.Equal(t, sqlClientSpan.TraceID(), rowsSpan.TraceID())
	require.Equal(t, sqlClientSpan.SpanID(), rowsSpan.ParentSpanID())

	// Assert on propagation (parent-child relationships)
	require.Equal(t, httpServerSpan.TraceID(), sqlClientSpan.TraceID(), "trace ID mismatch")
	require.Equal(t, httpServerSpan.SpanID(), sqlClientSpan.ParentSpanID(), "SQL client parent must be HTTP server")
	require.True(t, httpServerSpan.ParentSpanID().IsEmpty(), "HTTP server span must be the trace root")
}
