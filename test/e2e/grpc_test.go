//go:build e2e

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"fmt"
	"testing"

	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"

	"go.opentelemetry.io/otelc/test/testutil"
)

func TestGrpc(t *testing.T) {
	t.Parallel()
	f := testutil.NewTestFixture(t)

	port := testutil.FreePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	f.BuildAndStart("grpcserver", fmt.Sprintf("-port=%d", port))
	testutil.WaitForTCP(t, addr)

	f.BuildAndRun("grpcclient", "-addr", addr, "-name", "OpenTelemetry")
	f.Run("grpcclient", "-addr", addr, "-stream")
	f.WaitForSpans(4) // 2 traces (unary + stream) × 2 spans (client + server)

	f.RequireTraceCount(2)    // unary + stream
	f.RequireSpansPerTrace(2) // client + server per trace

	grpcClientSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsClient,
		testutil.HasAttribute(string(semconv.RPCSystemKey), "grpc"),
	)
	testutil.RequireGRPCClientSemconv(t, grpcClientSpan, "127.0.0.1", "greeter.Greeter", "SayHello", 0)

	grpcServerSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsServer,
		testutil.HasAttribute(string(semconv.RPCSystemKey), "grpc"),
	)
	testutil.RequireGRPCServerSemconv(t, grpcServerSpan, "greeter.Greeter", "SayHello", 0)
}
