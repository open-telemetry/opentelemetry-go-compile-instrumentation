// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rpc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type testRpcRequest struct {
	system        string
	service       string
	method        string
	serverAddress string
}

type testRpcAttrsGetter struct{}

func (g testRpcAttrsGetter) GetSystem(request testRpcRequest) string {
	return request.system
}

func (g testRpcAttrsGetter) GetService(request testRpcRequest) string {
	return request.service
}

func (g testRpcAttrsGetter) GetMethod(request testRpcRequest) string {
	return request.method
}

func (g testRpcAttrsGetter) GetServerAddress(request testRpcRequest) string {
	return request.serverAddress
}

func TestRpcAttrsGetter(t *testing.T) {
	getter := testRpcAttrsGetter{}

	tests := []struct {
		name    string
		request testRpcRequest
		want    struct {
			system        string
			service       string
			method        string
			serverAddress string
		}
	}{
		{
			name: "grpc request",
			request: testRpcRequest{
				system:        "grpc",
				service:       "/helloworld.Greeter",
				method:        "SayHello",
				serverAddress: "localhost:50051",
			},
			want: struct {
				system        string
				service       string
				method        string
				serverAddress string
			}{
				system:        "grpc",
				service:       "/helloworld.Greeter",
				method:        "SayHello",
				serverAddress: "localhost:50051",
			},
		},
		{
			name: "empty request",
			request: testRpcRequest{
				system:        "",
				service:       "",
				method:        "",
				serverAddress: "",
			},
			want: struct {
				system        string
				service       string
				method        string
				serverAddress string
			}{
				system:        "",
				service:       "",
				method:        "",
				serverAddress: "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want.system, getter.GetSystem(tt.request))
			require.Equal(t, tt.want.service, getter.GetService(tt.request))
			require.Equal(t, tt.want.method, getter.GetMethod(tt.request))
			require.Equal(t, tt.want.serverAddress, getter.GetServerAddress(tt.request))
		})
	}
}
