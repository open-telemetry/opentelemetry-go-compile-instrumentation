//go:build e2e

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"

	"go.opentelemetry.io/otelc/test/testutil"
)

func TestHTTPServerDBClient(t *testing.T) {
	f := testutil.NewTestFixture(t)

	frontPort := testutil.FreePort(t)
	addr := fmt.Sprintf("http://127.0.0.1:%d", frontPort)

	f.BuildAndStart("httpserverdbclient", fmt.Sprintf("-front-port=%d", frontPort))
	testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", frontPort))

	f.BuildAndRun("httpclient", "-addr", addr, "-name", "test")

	// BuildAndRun returns once the client exits, but the server span ends after
	// the response is written, so it may still be in flight. Wait for all three.
	f.WaitForSpans(3)

	// One distributed trace with three spans:
	// HTTP client -> HTTP server -> SQL client
	f.RequireTraceCount(1)
	f.RequireSpansPerTrace(3)

	httpClientSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsClient,
		testutil.HasAttributeContaining(string(semconv.URLFullKey), "/hello"),
	)
	httpServerSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsServer,
		testutil.HasAttribute(string(semconv.URLPathKey), "/hello"),
		testutil.HasAttribute(string(semconv.HTTPResponseStatusCodeKey), int64(200)),
	)
	sqlClientSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsClient,
		testutil.HasAttribute(string(semconv.DBOperationNameKey), "SELECT"),
	)

	require.Equal(t, httpClientSpan.TraceID(), httpServerSpan.TraceID(), "HTTP client and server must share a trace ID")
	require.Equal(
		t,
		httpServerSpan.TraceID(),
		sqlClientSpan.TraceID(),
		"HTTP server and SQL client must share a trace ID",
	)
	require.Equal(t, httpClientSpan.SpanID(), httpServerSpan.ParentSpanID(), "HTTP server parent must be HTTP client")
	require.Equal(t, httpServerSpan.SpanID(), sqlClientSpan.ParentSpanID(), "SQL client parent must be HTTP server")
	require.True(t, httpClientSpan.ParentSpanID().IsEmpty(), "HTTP client span must be the trace root")
}
