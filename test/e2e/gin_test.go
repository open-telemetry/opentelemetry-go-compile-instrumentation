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

func TestGin(t *testing.T) {
	f := testutil.NewTestFixture(t)

	port := testutil.FreePort(t)

	f.BuildAndStart("ginserver", fmt.Sprintf("-port=%d", port))
	testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", port))

	f.BuildAndRun("httpclient", "-addr", fmt.Sprintf("http://127.0.0.1:%d", port), "-path", "/hello/OTel")
	testutil.WaitForSpanFlush(t)

	f.RequireTraceCount(1)
	f.RequireSpansPerTrace(2)

	clientSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsClient,
		testutil.HasAttributeContaining(string(semconv.URLFullKey), "/hello/OTel"),
	)

	serverSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsServer,
		testutil.HasAttribute(string(semconv.URLPathKey), "/hello/OTel"),
	)

	// Verify Gin-specific route attribute — this is the unique value that Gin provides.
	// A plain net/http server would record the literal URL path, not the route pattern.
	testutil.RequireAttribute(t, serverSpan, string(semconv.HTTPRouteKey), "/hello/:name")
	require.Equal(t, "GET /hello/:name", serverSpan.Name(),
		"span name must be route pattern, not literal URL")

	// Both spans must share the same trace ID, proving context propagation worked.
	require.Equal(t, clientSpan.TraceID(), serverSpan.TraceID())
}
