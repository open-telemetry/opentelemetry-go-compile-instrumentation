// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"go.opentelemetry.io/otelc/test/testutil"
)

func TestComplexGRPCApp(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "complexgrpc", "go", "build", "-a")

	f := testutil.NewTestFixture(t)
	frontPort := testutil.FreePort(t)
	backPort := testutil.FreePort(t)

	f.Start("complexgrpc", fmt.Sprintf("-front-port=%d", frontPort), fmt.Sprintf("-back-port=%d", backPort))
	testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", frontPort))
	testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", backPort))

	// Send request to frontend
	url := fmt.Sprintf("http://127.0.0.1:%d/hello", frontPort)
	resp, err := http.Get(url)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Wait for the spans from the frontend, client, and backend to be flushed
	time.Sleep(3 * time.Second)
	testutil.WaitForSpanFlush(t)

	// We expect exactly 1 trace with 3 spans:
	// 1. HTTP server (Frontend)
	// 2. gRPC client (Frontend -> Backend)
	// 3. gRPC server (Backend)
	f.RequireTraceCount(1)
	f.RequireSpansPerTrace(3)

	httpServerSpan := testutil.RequireSpan(t, f.Traces(), testutil.IsServer, func(s ptrace.Span) bool { return s.Name() == "GET" })
	grpcClientSpan := testutil.RequireSpan(t, f.Traces(), testutil.IsClient)
	grpcServerSpan := testutil.RequireSpan(t, f.Traces(), testutil.IsServer, func(s ptrace.Span) bool { return s.Name() == "greeter.Greeter/SayHello" })

	// Assert on propagation (parent-child relationships)
	require.Equal(t, httpServerSpan.TraceID(), grpcClientSpan.TraceID(), "trace ID mismatch")
	require.Equal(t, httpServerSpan.TraceID(), grpcServerSpan.TraceID(), "trace ID mismatch")

	require.Equal(t, httpServerSpan.SpanID(), grpcClientSpan.ParentSpanID(), "gRPC client parent must be HTTP server")
	require.Equal(t, grpcClientSpan.SpanID(), grpcServerSpan.ParentSpanID(), "gRPC server parent must be gRPC client")
}
