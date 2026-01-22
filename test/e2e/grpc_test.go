//go:build e2e

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"testing"
	"time"

	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/testutil"
)

func TestGrpc(t *testing.T) {
	f := testutil.NewTestFixture(t)

	f.BuildAndStart("grpcserver")
	time.Sleep(time.Second)

	f.BuildAndRun("grpcclient", "-addr", "127.0.0.1:50051", "-name", "OpenTelemetry")
	f.Run("grpcclient", "-addr", "127.0.0.1:50051", "-stream")

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
