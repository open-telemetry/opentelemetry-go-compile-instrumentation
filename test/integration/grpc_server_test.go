// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"io"
	"testing"
	"time"

	pb "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/apps/grpcserver/pb"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/testutil"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestGRPCServer(t *testing.T) {
	testCases := []struct {
		name     string
		method   string
		exercise func(t *testing.T, client *GRPCClient)
	}{
		{
			name:   "unary",
			method: "SayHello",
			exercise: func(t *testing.T, client *GRPCClient) {
				client.SayHello(t, "TestUser")
			},
		},
		{
			name:   "streaming",
			method: "SayHelloStream",
			exercise: func(t *testing.T, client *GRPCClient) {
				client.SayHelloStream(t, "StreamUser", 3)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f := testutil.NewTestFixture(t)

			f.BuildAndStart("grpcserver")
			time.Sleep(2 * time.Second)

			client := NewGRPCClient(t, "localhost:50051")
			tc.exercise(t, client)
			time.Sleep(100 * time.Millisecond)

			span := f.RequireSingleSpan()
			testutil.RequireGRPCServerSemconv(t, span, "greeter.Greeter", tc.method, 0)
		})
	}
}

// GRPCClient wraps a test gRPC client connection.
type GRPCClient struct {
	conn   *grpc.ClientConn
	client pb.GreeterClient
}

// NewGRPCClient creates a new test gRPC client connected to the given address.
// The connection is automatically closed when the test completes.
func NewGRPCClient(t *testing.T, addr string) *GRPCClient {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })

	return &GRPCClient{
		conn:   conn,
		client: pb.NewGreeterClient(conn),
	}
}

// SayHello sends a unary request and validates the response.
func (c *GRPCClient) SayHello(t *testing.T, name string) {
	resp, err := c.client.SayHello(t.Context(), &pb.HelloRequest{Name: name})
	require.NoError(t, err)
	require.Contains(t, resp.GetMessage(), name)
}

// SayHelloStream sends multiple streaming requests and validates responses.
func (c *GRPCClient) SayHelloStream(t *testing.T, name string, count int) {
	stream, err := c.client.SayHelloStream(t.Context())
	require.NoError(t, err)

	for i := 0; i < count; i++ {
		err := stream.Send(&pb.HelloRequest{Name: name})
		require.NoError(t, err)
	}
	require.NoError(t, stream.CloseSend())

	responseCount := 0
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		require.Contains(t, resp.GetMessage(), name)
		responseCount++
	}
	require.Equal(t, count, responseCount, "Should receive %d responses", count)
}
