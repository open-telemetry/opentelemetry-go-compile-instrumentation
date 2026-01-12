// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"context"
	"io"
	"net"
	"testing"

	pb "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/demo/grpc/server/pb"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/app"
)

// testGreeterServer is a simple non-instrumented gRPC server for testing.
type testGreeterServer struct {
	pb.UnimplementedGreeterServer
}

func (s *testGreeterServer) SayHello(ctx context.Context, req *pb.HelloRequest) (*pb.HelloReply, error) {
	return &pb.HelloReply{Message: "Hello " + req.GetName()}, nil
}

func (s *testGreeterServer) SayHelloStream(stream grpc.BidiStreamingServer[pb.HelloRequest, pb.HelloReply]) error {
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err := stream.Send(&pb.HelloReply{Message: "Hello " + req.GetName()}); err != nil {
			return err
		}
	}
}

// TestGRPCClientInstrumentation tests gRPC client instrumentation in isolation.
// Uses a non-instrumented in-process test server (like httptest.Server).
// Expects: 1 trace with 1 client span.
func TestGRPCClientInstrumentation(t *testing.T) {
	f := app.NewE2EFixture(t)

	// Start a non-instrumented test gRPC server
	lis, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)

	grpcServer := grpc.NewServer()
	pb.RegisterGreeterServer(grpcServer, &testGreeterServer{})

	go func() {
		_ = grpcServer.Serve(lis)
	}()
	defer grpcServer.Stop()

	serverAddr := lis.Addr().String()

	// Build and run the instrumented client
	f.Build("grpc/client")
	output := f.RunClient("grpc/client", "-addr="+serverAddr, "-name=ClientTest")

	require.Contains(t, output, `"msg":"greeting"`)
	require.Contains(t, output, `"message":"Hello ClientTest"`)

	span := f.RequireSingleSpan()
	app.RequireGRPCClientSemconv(t, span, "127.0.0.1")
}

// TestGRPCClientStreaming tests gRPC client streaming in isolation.
func TestGRPCClientStreaming(t *testing.T) {
	f := app.NewE2EFixture(t)

	// Start a non-instrumented test gRPC server
	lis, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)

	grpcServer := grpc.NewServer()
	pb.RegisterGreeterServer(grpcServer, &testGreeterServer{})

	go func() {
		_ = grpcServer.Serve(lis)
	}()
	defer grpcServer.Stop()

	serverAddr := lis.Addr().String()

	// Build and run the instrumented client
	f.Build("grpc/client")
	output := f.RunClient("grpc/client", "-addr="+serverAddr, "-stream", "-count=3")
	require.Contains(t, output, "stream response")

	span := f.RequireSingleSpan()
	app.RequireGRPCClientSemconv(t, span, "127.0.0.1")
}
