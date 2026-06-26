// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/shared/grpcpb/pb"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/testutil"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
	"github.com/stretchr/testify/require"
	semconv "go.opentelemetry.io/otel/semconv/v1.20.0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestGRPCServer(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "grpcserver", "go", "build", "-a")

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
			port := testutil.FreePort(t)
			addr := fmt.Sprintf("localhost:%d", port)

			f.Start("grpcserver", fmt.Sprintf("-port=%d", port))
			testutil.WaitForTCP(t, addr)

			client := NewGRPCClient(t, addr)
			tc.exercise(t, client)
			testutil.WaitForSpanFlush(t)

			span := f.RequireSingleSpan()
			testutil.RequireGRPCServerSemconv(t, span, "greeter.Greeter", tc.method, 0)
		})
	}

	// This test verifies that telemetry is properly flushed
	// when the server receives SIGINT, using the batch span processor.
	// This test validates that the signal-based shutdown handler in the instrumentation
	// layer correctly triggers a flush before exit.
	t.Run("telemetry flush on signal", func(t *testing.T) {
		if util.IsWindows() {
			t.Skip("SIGINT is not supported on windows")
		}

		f := testutil.NewTestFixture(t)
		f.SetEnv("OTEL_GO_SIMPLE_SPAN_PROCESSOR", "false")

		port := testutil.FreePort(t)
		addr := fmt.Sprintf("localhost:%d", port)
		srv := f.Start("grpcserver", fmt.Sprintf("-port=%d", port))
		testutil.WaitForTCP(t, addr)

		client := NewGRPCClient(t, addr)
		client.SayHello(t, "ShutdownTest")

		require.NoError(t, srv.Cmd.Process.Signal(os.Interrupt))
		waitForProcessExit(t, srv.Cmd, 10*time.Second)
		testutil.WaitForSpanFlush(t)

		spans := testutil.AllSpans(f.Traces())
		require.NotEmpty(t, spans, "expected spans to be flushed on SIGINT shutdown")

		serverSpan := testutil.RequireSpan(t, f.Traces(),
			testutil.IsServer,
			testutil.HasAttribute(string(semconv.RPCSystemKey), "grpc"),
		)
		testutil.RequireGRPCServerSemconv(t, serverSpan, "greeter.Greeter", "SayHello", 0)
	})
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

	for range count {
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

// waitForProcessExit waits for a process to exit within the given timeout.
func waitForProcessExit(t *testing.T, cmd *exec.Cmd, timeout time.Duration) {
	t.Helper()

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatal("process did not exit within timeout")
	}
}
