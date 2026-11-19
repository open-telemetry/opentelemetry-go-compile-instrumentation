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

func TestGRPCServerDBClient(t *testing.T) {
	f := testutil.NewTestFixture(t)

	frontPort := testutil.FreePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", frontPort)

	f.BuildAndStart("grpcserverdbclient", fmt.Sprintf("-front-port=%d", frontPort))
	testutil.WaitForTCP(t, addr)

	f.BuildAndRun("grpcclient", "-addr", addr, "-name", "test")

	// The client can exit before the server span ends.
	f.WaitForSpans(4)

	f.RequireTraceCount(1)
	f.RequireSpansPerTrace(4)

	grpcClientSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsClient,
		testutil.HasAttribute(string(semconv.RPCSystemKey), "grpc"),
	)
	grpcServerSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsServer,
		testutil.HasAttribute(string(semconv.RPCSystemKey), "grpc"),
		testutil.HasAttribute(string(semconv.RPCGRPCStatusCodeKey), int64(0)),
	)
	sqlClientSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsClient,
		testutil.HasAttribute(string(semconv.DBOperationNameKey), "SELECT"),
	)

	require.Equal(t, grpcClientSpan.TraceID(), grpcServerSpan.TraceID(), "gRPC client and server must share a trace ID")
	require.Equal(
		t,
		grpcServerSpan.TraceID(),
		sqlClientSpan.TraceID(),
		"gRPC server and SQL client must share a trace ID",
	)
	rowsSpan := testutil.RequireSpan(t, f.Traces(), testutil.HasName("sql.rows"))
	require.Equal(t, sqlClientSpan.TraceID(), rowsSpan.TraceID())
	require.Equal(t, sqlClientSpan.SpanID(), rowsSpan.ParentSpanID())
	require.Equal(t, grpcClientSpan.SpanID(), grpcServerSpan.ParentSpanID(), "gRPC server parent must be gRPC client")
	require.Equal(t, grpcServerSpan.SpanID(), sqlClientSpan.ParentSpanID(), "SQL client parent must be gRPC server")
	require.True(t, grpcClientSpan.ParentSpanID().IsEmpty(), "gRPC client span must be the trace root")
}
