// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rpc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRpcSpanNameExtractor(t *testing.T) {
	getter := testRpcAttrsGetter{}
	extractor := &RpcSpanNameExtractor[testRpcRequest]{
		Getter: getter,
	}

	tests := []struct {
		name     string
		request  testRpcRequest
		expected string
	}{
		{
			name: "valid grpc request",
			request: testRpcRequest{
				system:        "grpc",
				service:       "/helloworld.Greeter",
				method:        "SayHello",
				serverAddress: "localhost:50051",
			},
			expected: "/helloworld.Greeter/SayHello",
		},
		{
			name: "empty service",
			request: testRpcRequest{
				system:        "grpc",
				service:       "",
				method:        "SayHello",
				serverAddress: "localhost:50051",
			},
			expected: "RPC request",
		},
		{
			name: "empty method",
			request: testRpcRequest{
				system:        "grpc",
				service:       "/helloworld.Greeter",
				method:        "",
				serverAddress: "localhost:50051",
			},
			expected: "RPC request",
		},
		{
			name: "both empty",
			request: testRpcRequest{
				system:        "grpc",
				service:       "",
				method:        "",
				serverAddress: "localhost:50051",
			},
			expected: "RPC request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spanName := extractor.Extract(tt.request)
			require.Equal(t, tt.expected, spanName)
		})
	}
}
