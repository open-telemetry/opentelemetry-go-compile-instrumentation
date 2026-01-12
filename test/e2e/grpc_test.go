//go:build e2e

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"testing"

	"github.com/stretchr/testify/require"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/app"
)

func TestGrpc(t *testing.T) {
	// 1. Setup fixture (starts collector + configures OTEL env vars)
	f := app.NewE2EFixture(t)

	// 2. Build the server and client applications with the instrumentation tool
	f.Build("grpc/server")
	f.Build("grpc/client")

	// 3. Start the server and wait for it to be ready
	server := f.StartServer("grpc/server")

	// 4. Run the client to make a unary RPC call
	f.RunClient("grpc/client", "-name", "OpenTelemetry")

	// 5. Run the client again for streaming RPC
	f.RunClient("grpc/client", "-stream")

	// 6. Send shutdown request to the server
	f.RunClient("grpc/client", "-shutdown")

	// 7. Stop server and verify instrumentation was initialized
	output := server.Stop()
	require.Contains(t, output, "gRPC server instrumentation initialized", "instrumentation should be initialized")
	require.Contains(t, output, `"msg":"server listening"`)
	require.Contains(t, output, `"msg":"received request"`)

	// 8. Verify trace counts
	// We make 3 requests: unary call, stream call, and shutdown
	// Each request generates 1 trace with 2 spans (client + server)
	f.RequireTraceCount(3)
	f.RequireSpansPerTrace(2)

	// 9. Verify gRPC client span semantic conventions
	grpcClientSpan := app.RequireSpan(t, f.Traces(),
		app.IsClient,
		app.HasAttribute(string(semconv.RPCSystemKey), "grpc"),
	)
	app.RequireGRPCClientSemconv(t, grpcClientSpan, "::1")

	// 10. Verify gRPC server span semantic conventions
	grpcServerSpan := app.RequireSpan(t, f.Traces(),
		app.IsServer,
		app.HasAttribute(string(semconv.RPCSystemKey), "grpc"),
	)
	app.RequireGRPCServerSemconv(t, grpcServerSpan)
}
