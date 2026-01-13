// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"io"
	"testing"
	"time"

	pb "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/apps/grpcserver/pb"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/app"
)

func TestGRPCServerInstrumentation(t *testing.T) {
	f := app.NewE2EFixture(t)

	f.BuildApp("grpcserver")
	f.StartApp("grpcserver")
	time.Sleep(2 * time.Second)

	conn, err := grpc.NewClient("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()
	client := pb.NewGreeterClient(conn)

	resp, err := client.SayHello(t.Context(), &pb.HelloRequest{Name: "TestUser"})
	require.NoError(t, err)
	require.Contains(t, resp.GetMessage(), "TestUser")
	time.Sleep(100 * time.Millisecond)

	span := f.RequireSingleSpan()
	app.RequireGRPCServerSemconv(t, span)
}

func TestGRPCServerStreaming(t *testing.T) {
	f := app.NewE2EFixture(t)

	f.BuildApp("grpcserver")
	f.StartApp("grpcserver")
	time.Sleep(2 * time.Second)

	conn, err := grpc.NewClient("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()
	client := pb.NewGreeterClient(conn)
	stream, err := client.SayHelloStream(t.Context())
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
	time.Sleep(100 * time.Millisecond)

	span := f.RequireSingleSpan()
	app.RequireGRPCServerSemconv(t, span)
}
