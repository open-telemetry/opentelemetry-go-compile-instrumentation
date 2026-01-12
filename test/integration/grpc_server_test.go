// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"context"
	"io"
	"testing"
	"time"

	pb "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/demo/grpc/server/pb"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/app"
)

// TestGRPCServerInstrumentation tests gRPC server instrumentation in isolation.
// Uses a non-instrumented gRPC client directly in the test code.
// Expects: 2 traces (SayHello and Shutdown), each with 1 server span.
func TestGRPCServerInstrumentation(t *testing.T) {
	f := app.NewE2EFixture(t)

	// Build server WITH instrumentation
	f.Build("grpc/server")

	// Start the instrumented server
	server := f.StartServer("grpc/server")

	// Create a non-instrumented gRPC client
	conn, err := grpc.NewClient("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	client := pb.NewGreeterClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Make a unary RPC call
	resp, err := client.SayHello(ctx, &pb.HelloRequest{Name: "TestUser"})
	require.NoError(t, err)
	require.Contains(t, resp.GetMessage(), "TestUser")

	// Shutdown the server
	_, err = client.Shutdown(ctx, &pb.ShutdownRequest{})
	require.NoError(t, err)

	serverOutput := server.Stop()
	t.Logf("Server output:\n%s", serverOutput)

	// We expect 2 traces: one for SayHello and one for Shutdown
	f.RequireTraceCount(2)

	// Find and verify the SayHello span (not the Shutdown span)
	span := app.RequireSpan(t, f.Traces(),
		app.IsServer,
		app.HasAttribute("rpc.method", "SayHello"),
	)
	app.RequireGRPCServerSemconv(t, span)
}

// TestGRPCServerStreaming tests gRPC server streaming in isolation.
// Uses a non-instrumented gRPC client directly in the test code.
// Expects: 2 traces (SayHelloStream and Shutdown), each with 1 server span.
func TestGRPCServerStreaming(t *testing.T) {
	f := app.NewE2EFixture(t)

	// Build server WITH instrumentation
	f.Build("grpc/server")

	// Start the instrumented server
	server := f.StartServer("grpc/server")

	// Create a non-instrumented gRPC client
	conn, err := grpc.NewClient("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	client := pb.NewGreeterClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Make a streaming RPC call
	stream, err := client.SayHelloStream(ctx)
	require.NoError(t, err)

	// Send 3 requests
	for i := 0; i < 3; i++ {
		err := stream.Send(&pb.HelloRequest{Name: "StreamUser"})
		require.NoError(t, err)
	}
	require.NoError(t, stream.CloseSend())

	// Receive responses
	responseCount := 0
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		require.Contains(t, resp.GetMessage(), "StreamUser")
		responseCount++
	}
	require.Equal(t, 3, responseCount, "Should receive 3 responses")

	// Shutdown the server
	_, err = client.Shutdown(ctx, &pb.ShutdownRequest{})
	require.NoError(t, err)

	serverOutput := server.Stop()
	t.Logf("Server output:\n%s", serverOutput)

	// We expect 2 traces: one for SayHelloStream and one for Shutdown
	f.RequireTraceCount(2)

	// Find and verify the SayHelloStream span (not the Shutdown span)
	span := app.RequireSpan(t, f.Traces(),
		app.IsServer,
		app.HasAttribute("rpc.method", "SayHelloStream"),
	)
	app.RequireGRPCServerSemconv(t, span)
}
