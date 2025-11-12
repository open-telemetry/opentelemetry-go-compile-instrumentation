// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package grpc

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/propagation"
	"google.golang.org/grpc/metadata"
)

func TestGrpcRequest(t *testing.T) {
	md := metadata.MD{
		"key1": []string{"value1"},
		"key2": []string{"value2"},
	}
	carrier := &grpcMetadataCarrier{metadata: &md}

	req := grpcRequest{
		methodName:    "/helloworld.Greeter/SayHello",
		serverAddress: "localhost:50051",
		propagators:   carrier,
	}

	require.Equal(t, "/helloworld.Greeter/SayHello", req.methodName)
	require.Equal(t, "localhost:50051", req.serverAddress)
	require.NotNil(t, req.propagators)
}

func TestGrpcResponse(t *testing.T) {
	resp := grpcResponse{
		statusCode: 0, // OK
	}

	require.Equal(t, 0, resp.statusCode)
}

func TestGrpcMetadataCarrier_Get(t *testing.T) {
	md := metadata.MD{
		"key1": []string{"value1"},
		"key2": []string{"value2", "value3"},
	}
	carrier := &grpcMetadataCarrier{metadata: &md}

	require.Equal(t, "value1", carrier.Get("key1"))
	require.Equal(t, "value2", carrier.Get("key2")) // Returns first value
	require.Equal(t, "", carrier.Get("nonexistent"))
}

func TestGrpcMetadataCarrier_Set(t *testing.T) {
	md := metadata.MD{}
	carrier := &grpcMetadataCarrier{metadata: &md}

	carrier.Set("key1", "value1")
	carrier.Set("key2", "value2")

	require.Equal(t, []string{"value1"}, (*carrier.metadata)["key1"])
	require.Equal(t, []string{"value2"}, (*carrier.metadata)["key2"])
}

func TestGrpcMetadataCarrier_Keys(t *testing.T) {
	md := metadata.MD{
		"key1": []string{"value1"},
		"key2": []string{"value2"},
		"key3": []string{"value3"},
	}
	carrier := &grpcMetadataCarrier{metadata: &md}

	keys := carrier.Keys()
	require.Len(t, keys, 3)
	require.Contains(t, keys, "key1")
	require.Contains(t, keys, "key2")
	require.Contains(t, keys, "key3")
}

func TestGrpcMetadataCarrier_PropagationCompatibility(t *testing.T) {
	// Test that grpcMetadataCarrier implements propagation.TextMapCarrier
	md := metadata.MD{}
	var carrier propagation.TextMapCarrier = &grpcMetadataCarrier{metadata: &md}

	carrier.Set("traceparent", "00-trace-id-span-id-01")
	carrier.Set("tracestate", "vendor=value")

	require.Equal(t, "00-trace-id-span-id-01", carrier.Get("traceparent"))
	require.Equal(t, "vendor=value", carrier.Get("tracestate"))
}
