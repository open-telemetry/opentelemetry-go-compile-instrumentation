// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"log/slog"
	"os"
	"testing"

	pb "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/demo/grpc/server/pb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	// Initialize logger for tests
	logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError, // Quiet during tests
	}))
}

func TestSayHello(t *testing.T) {
	s := &server{}
	ctx := context.Background()

	tests := []struct {
		name     string
		request  string
		expected string
	}{
		{
			name:     "simple greeting",
			request:  "world",
			expected: "Hello world",
		},
		{
			name:     "named greeting",
			request:  "Alice",
			expected: "Hello Alice",
		},
		{
			name:     "empty name",
			request:  "",
			expected: "Hello ",
		},
		{
			name:     "special characters",
			request:  "世界",
			expected: "Hello 世界",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := s.SayHello(ctx, &pb.HelloRequest{Name: tt.request})
			require.NoError(t, err)
			assert.Equal(t, tt.expected, resp.GetMessage())
		})
	}
}

func TestShutdown(t *testing.T) {
	s := &server{}
	ctx := context.Background()

	// Note: We can't test the os.Exit() behavior in a unit test,
	// but we can test that the RPC returns the correct response
	resp, err := s.Shutdown(ctx, &pb.ShutdownRequest{})
	require.NoError(t, err)
	assert.Equal(t, "Server is shutting down", resp.GetMessage())
}
