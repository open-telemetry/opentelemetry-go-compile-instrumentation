// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"go.opentelemetry.io/otelc/test/shared/grpcpb/pb"
	"go.opentelemetry.io/otelc/test/testutil"
)

func TestGRPCServerDBClient(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "grpcserverdbclient", "go", "build", "-a")

	f := testutil.NewTestFixture(t)
	frontPort := testutil.FreePort(t)

	f.Start("grpcserverdbclient", fmt.Sprintf("-front-port=%d", frontPort))
	testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", frontPort))

	// Connect to frontend
	conn, err := grpc.NewClient(
		fmt.Sprintf("127.0.0.1:%d", frontPort),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	defer conn.Close()
	client := pb.NewGreeterClient(conn)

	// Send request
	resp, err := client.SayHello(t.Context(), &pb.HelloRequest{Name: "test"})
	require.NoError(t, err)
	require.Equal(t, "frontend querying database", resp.GetMessage())

	// Wait for the spans to be flushed
	testutil.WaitForSpanFlush(t)

	// We expect exactly 1 trace with 2 spans:
	// 1. gRPC server (Frontend)
	// 2. SQL client (Frontend -> Database)
	f.RequireTraceCount(1)
	f.RequireSpansPerTrace(2)

	grpcServerSpan := testutil.RequireSpan(
		t,
		f.Traces(),
		testutil.IsServer,
		func(s ptrace.Span) bool { return s.Name() == "greeter.Greeter/SayHello" },
	)
	sqlClientSpan := testutil.RequireSpan(t, f.Traces(), testutil.IsClient)

	// Assert on propagation (parent-child relationships)
	require.Equal(t, grpcServerSpan.TraceID(), sqlClientSpan.TraceID(), "trace ID mismatch")
	require.Equal(t, grpcServerSpan.SpanID(), sqlClientSpan.ParentSpanID(), "SQL client parent must be gRPC server")
}
