// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package grpc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGrpcServerAttrsGetter_GetSystem(t *testing.T) {
	getter := grpcServerAttrsGetter{}
	request := grpcRequest{
		methodName:    "/helloworld.Greeter/SayHello",
		serverAddress: "localhost:50051",
	}

	system := getter.GetSystem(request)
	require.Equal(t, "grpc", system)
}

func TestGrpcServerAttrsGetter_GetService(t *testing.T) {
	getter := grpcServerAttrsGetter{}

	tests := []struct {
		name       string
		methodName string
		expected   string
	}{
		{
			name:       "standard grpc method",
			methodName: "/helloworld.Greeter/SayHello",
			expected:   "/helloworld.Greeter",
		},
		{
			name:       "nested package",
			methodName: "/com.example.service.v1.API/CreateUser",
			expected:   "/com.example.service.v1.API",
		},
		{
			name:       "no slash",
			methodName: "InvalidMethod",
			expected:   "",
		},
		{
			name:       "only one slash",
			methodName: "/Service",
			expected:   "",
		},
		{
			name:       "empty string",
			methodName: "",
			expected:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := grpcRequest{
				methodName:    tt.methodName,
				serverAddress: "localhost:50051",
			}

			service := getter.GetService(request)
			require.Equal(t, tt.expected, service)
		})
	}
}

func TestGrpcServerAttrsGetter_GetMethod(t *testing.T) {
	getter := grpcServerAttrsGetter{}

	tests := []struct {
		name       string
		methodName string
		expected   string
	}{
		{
			name:       "standard grpc method",
			methodName: "/helloworld.Greeter/SayHello",
			expected:   "SayHello",
		},
		{
			name:       "nested package",
			methodName: "/com.example.service.v1.API/CreateUser",
			expected:   "CreateUser",
		},
		{
			name:       "no slash",
			methodName: "InvalidMethod",
			expected:   "",
		},
		{
			name:       "only one slash",
			methodName: "/Service",
			expected:   "Service",
		},
		{
			name:       "empty string",
			methodName: "",
			expected:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := grpcRequest{
				methodName:    tt.methodName,
				serverAddress: "localhost:50051",
			}

			method := getter.GetMethod(request)
			require.Equal(t, tt.expected, method)
		})
	}
}

func TestGrpcServerAttrsGetter_GetServerAddress(t *testing.T) {
	getter := grpcServerAttrsGetter{}

	tests := []struct {
		name          string
		serverAddress string
	}{
		{
			name:          "localhost with port",
			serverAddress: "localhost:50051",
		},
		{
			name:          "ip address with port",
			serverAddress: "127.0.0.1:50051",
		},
		{
			name:          "empty address",
			serverAddress: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := grpcRequest{
				methodName:    "/helloworld.Greeter/SayHello",
				serverAddress: tt.serverAddress,
			}

			address := getter.GetServerAddress(request)
			require.Equal(t, tt.serverAddress, address)
		})
	}
}
