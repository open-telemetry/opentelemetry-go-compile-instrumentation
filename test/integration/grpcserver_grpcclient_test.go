// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"go.opentelemetry.io/otelc/test/shared/grpcpb/pb"
	"go.opentelemetry.io/otelc/test/testutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestGRPCServerGRPCClient(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "grpcservergrpcclient", "go", "build", "-a")

	f := testutil.NewTestFixture(t)
	frontPort := testutil.FreePort(t)
	backPort := testutil.FreePort(t)

	f.Start("grpcservergrpcclient", fmt.Sprintf("-front-port=%d", frontPort), fmt.Sprintf("-back-port=%d", backPort))
	testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", frontPort))
	testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", backPort))

	// Send request to frontend via gRPC
	conn, err := grpc.NewClient(fmt.Sprintf("127.0.0.1:%d", frontPort), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	client := pb.NewGreeterClient(conn)
	resp, err := client.SayHello(t.Context(), &pb.HelloRequest{Name: "otelc"})
	require.NoError(t, err)
	require.Contains(t, resp.GetMessage(), "frontend calling backend")

	// Wait for the spans from the frontend, client, and backend to be flushed
	testutil.WaitForSpanFlush(t)

	// We expect exactly 1 trace with 3 spans:
	// 1. gRPC server (Frontend)
	// 2. gRPC client (Frontend -> Backend)
	// 3. gRPC server (Backend)
	f.RequireTraceCount(1)
	f.RequireSpansPerTrace(3)

	grpcClientSpan := testutil.RequireSpan(t, f.Traces(), testutil.IsClient)
	
	// Collect all grpc server spans manually since testutil.RequireSpans is undefined
	var grpcServerSpans []ptrace.Span
	for i := 0; i < f.Traces().ResourceSpans().Len(); i++ {
		rs := f.Traces().ResourceSpans().At(i)
		for j := 0; j < rs.ScopeSpans().Len(); j++ {
			ss := rs.ScopeSpans().At(j)
			for k := 0; k < ss.Spans().Len(); k++ {
				span := ss.Spans().At(k)
				if span.Kind() == ptrace.SpanKindServer && span.Name() == "greeter.Greeter/SayHello" {
					grpcServerSpans = append(grpcServerSpans, span)
				}
			}
		}
	}
	require.Len(t, grpcServerSpans, 2, "expected exactly 2 gRPC server spans")

	// Find the frontend server span (it is the parent of the grpcClientSpan)
	var frontendServerSpan ptrace.Span
	var backendServerSpan ptrace.Span
	
	for _, span := range grpcServerSpans {
		if span.SpanID() == grpcClientSpan.ParentSpanID() {
			frontendServerSpan = span
		} else {
			backendServerSpan = span
		}
	}
	require.False(t, frontendServerSpan.SpanID().IsEmpty(), "could not find frontend server span")
	require.False(t, backendServerSpan.SpanID().IsEmpty(), "could not find backend server span")

	// Assert on propagation (parent-child relationships)
	require.Equal(t, frontendServerSpan.TraceID(), grpcClientSpan.TraceID(), "trace ID mismatch")
	require.Equal(t, frontendServerSpan.TraceID(), backendServerSpan.TraceID(), "trace ID mismatch")

	require.Equal(t, frontendServerSpan.SpanID(), grpcClientSpan.ParentSpanID(), "gRPC client parent must be Frontend server")
	require.Equal(t, grpcClientSpan.SpanID(), backendServerSpan.ParentSpanID(), "Backend server parent must be gRPC client")
}
