// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"context"
	"io"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/app"
	pb "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/apps/grpcserver/pb"
)

func TestGRPCClient(t *testing.T) {
	testCases := []struct {
		name           string
		extraArgs      []string
		expectedOutput string
	}{
		{
			name:           "unary",
			extraArgs:      []string{"-name=ClientTest"},
			expectedOutput: "Hello ClientTest",
		},
		{
			name:           "streaming",
			extraArgs:      []string{"-stream", "-count=3"},
			expectedOutput: "stream response",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f := app.NewE2EFixture(t)
			server := StartGRPCServer(t)

			args := append([]string{"-addr=" + server.Addr}, tc.extraArgs...)
			f.BuildApp("grpcclient")
			output := f.RunApp("grpcclient", args...)

			require.Contains(t, output, tc.expectedOutput)
			span := f.RequireSingleSpan()
			app.RequireGRPCClientSemconv(t, span, "127.0.0.1")
		})
	}
}

// GRPCServer wraps a test gRPC server with its address.
type GRPCServer struct {
	*grpc.Server
	Addr     string
	listener net.Listener
}

// Stop gracefully stops the gRPC server.
func (s *GRPCServer) Stop() {
	s.Server.Stop()
}

// testGreeterServer is a simple non-instrumented gRPC server for testing.
type testGreeterServer struct {
	pb.UnimplementedGreeterServer
}

func (s *testGreeterServer) SayHello(_ context.Context, req *pb.HelloRequest) (*pb.HelloReply, error) {
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

// StartGRPCServer creates and starts a test gRPC server.
// The server is automatically stopped when the test completes.
func StartGRPCServer(t *testing.T, opts ...grpc.ServerOption) *GRPCServer {
	t.Helper()

	lis, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)

	server := grpc.NewServer(opts...)
	pb.RegisterGreeterServer(server, &testGreeterServer{})

	go func() {
		_ = server.Serve(lis)
	}()

	t.Cleanup(server.Stop)

	return &GRPCServer{
		Server:   server,
		Addr:     lis.Addr().String(),
		listener: lis,
	}
}
