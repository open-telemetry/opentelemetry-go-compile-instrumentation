// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"

	pb "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/demo/grpc/server/pb"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func init() {
	// Initialize logger for tests
	logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError, // Quiet during tests
	}))
}

// mockGreeterClient implements pb.GreeterClient for testing
type mockGreeterClient struct {
	sayHelloFunc       func(ctx context.Context, req *pb.HelloRequest) (*pb.HelloReply, error)
	sayHelloStreamFunc func(ctx context.Context) (pb.Greeter_SayHelloStreamClient, error)
	shutdownFunc       func(ctx context.Context, req *pb.ShutdownRequest) (*pb.ShutdownReply, error)
}

func (m *mockGreeterClient) SayHello(
	ctx context.Context,
	req *pb.HelloRequest,
	opts ...grpc.CallOption,
) (*pb.HelloReply, error) {
	if m.sayHelloFunc != nil {
		return m.sayHelloFunc(ctx, req)
	}
	return &pb.HelloReply{Message: "Hello " + req.GetName()}, nil
}

func (m *mockGreeterClient) SayHelloStream(
	ctx context.Context,
	opts ...grpc.CallOption,
) (pb.Greeter_SayHelloStreamClient, error) {
	if m.sayHelloStreamFunc != nil {
		return m.sayHelloStreamFunc(ctx)
	}
	return nil, errors.New("not implemented")
}

func (m *mockGreeterClient) Shutdown(
	ctx context.Context,
	req *pb.ShutdownRequest,
	opts ...grpc.CallOption,
) (*pb.ShutdownReply, error) {
	if m.shutdownFunc != nil {
		return m.shutdownFunc(ctx, req)
	}
	return &pb.ShutdownReply{Message: "Server is shutting down"}, nil
}

// mockStreamClient implements pb.Greeter_SayHelloStreamClient for testing
type mockStreamClient struct {
	requests  []*pb.HelloRequest
	responses []*pb.HelloReply
	recvIndex int
	sendIndex int
	sendErr   error
	recvErr   error
}

func (m *mockStreamClient) Send(req *pb.HelloRequest) error {
	if m.sendErr != nil {
		return m.sendErr
	}
	m.requests = append(m.requests, req)
	return nil
}

func (m *mockStreamClient) Recv() (*pb.HelloReply, error) {
	if m.recvErr != nil {
		return nil, m.recvErr
	}
	if m.recvIndex >= len(m.responses) {
		return nil, io.EOF
	}
	resp := m.responses[m.recvIndex]
	m.recvIndex++
	return resp, nil
}

func (m *mockStreamClient) CloseSend() error {
	return nil
}

func (m *mockStreamClient) Header() (metadata.MD, error)  { return nil, nil }
func (m *mockStreamClient) Trailer() metadata.MD          { return nil }
func (m *mockStreamClient) Context() context.Context      { return context.Background() }
func (m *mockStreamClient) SendMsg(msg interface{}) error { return nil }
func (m *mockStreamClient) RecvMsg(msg interface{}) error { return nil }

func TestRunUnary(t *testing.T) {
	// Save original values
	origCount := count
	origName := name

	// Restore original values after test
	t.Cleanup(func() {
		count = origCount
		name = origName
	})

	tests := []struct {
		name        string
		count       int
		requestName string
		mockError   error
	}{
		{
			name:        "single request",
			count:       1,
			requestName: "Alice",
			mockError:   nil,
		},
		{
			name:        "multiple requests",
			count:       3,
			requestName: "Bob",
			mockError:   nil,
		},
		{
			name:        "request with error",
			count:       1,
			requestName: "Error",
			mockError:   errors.New("mock error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callCount := 0
			mockClient := &mockGreeterClient{
				sayHelloFunc: func(ctx context.Context, req *pb.HelloRequest) (*pb.HelloReply, error) {
					callCount++
					if tt.mockError != nil {
						return nil, tt.mockError
					}
					return &pb.HelloReply{Message: "Hello " + req.GetName()}, nil
				},
			}

			// Set test parameters
			testCount := tt.count
			testName := tt.requestName
			count = &testCount
			name = &testName

			ctx := context.Background()

			// Run the function
			runUnary(ctx, mockClient)

			// Verify expected number of calls were made
			assert.Equal(t, tt.count, callCount)
		})
	}
}

func TestRunStream(t *testing.T) {
	// Save original values
	origCount := count
	origName := name

	// Restore original values after test
	t.Cleanup(func() {
		count = origCount
		name = origName
	})

	tests := []struct {
		name        string
		count       int
		requestName string
	}{
		{
			name:        "single stream message",
			count:       1,
			requestName: "Alice",
		},
		{
			name:        "multiple stream messages",
			count:       3,
			requestName: "Bob",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			streamMock := &mockStreamClient{
				responses: make([]*pb.HelloReply, tt.count),
			}
			for i := 0; i < tt.count; i++ {
				streamMock.responses[i] = &pb.HelloReply{Message: "Hello " + tt.requestName}
			}

			mockClient := &mockGreeterClient{
				sayHelloStreamFunc: func(ctx context.Context) (pb.Greeter_SayHelloStreamClient, error) {
					return streamMock, nil
				},
			}

			// Set test parameters
			testCount := tt.count
			testName := tt.requestName
			count = &testCount
			name = &testName

			ctx := context.Background()

			// Run the function
			runStream(ctx, mockClient)

			// Verify expected number of messages were sent
			assert.Equal(t, tt.count, len(streamMock.requests))
		})
	}
}

func TestRunStreamWithErrors(t *testing.T) {
	// Save original values
	origCount := count
	origName := name

	// Restore original values after test
	t.Cleanup(func() {
		count = origCount
		name = origName
	})

	t.Run("send error", func(t *testing.T) {
		streamMock := &mockStreamClient{
			sendErr: errors.New("send error"),
		}

		mockClient := &mockGreeterClient{
			sayHelloStreamFunc: func(ctx context.Context) (pb.Greeter_SayHelloStreamClient, error) {
				return streamMock, nil
			},
		}

		testCount := 3
		testName := "test"
		count = &testCount
		name = &testName

		ctx := context.Background()

		// Should not panic
		runStream(ctx, mockClient)

		// Should have attempted only one send before breaking
		assert.LessOrEqual(t, len(streamMock.requests), 1)
	})

	t.Run("recv error", func(t *testing.T) {
		streamMock := &mockStreamClient{
			recvErr: errors.New("recv error"),
		}

		mockClient := &mockGreeterClient{
			sayHelloStreamFunc: func(ctx context.Context) (pb.Greeter_SayHelloStreamClient, error) {
				return streamMock, nil
			},
		}

		testCount := 3
		testName := "test"
		count = &testCount
		name = &testName

		ctx := context.Background()

		// Should not panic
		runStream(ctx, mockClient)

		// Verify sends completed
		assert.Equal(t, testCount, len(streamMock.requests))
	})
}
