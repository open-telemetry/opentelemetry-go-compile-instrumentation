// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rpc

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"
)

func TestRpcAttrsExtractor_OnStart(t *testing.T) {
	getter := testRpcAttrsGetter{}
	extractor := &RpcAttrsExtractor[testRpcRequest, any, testRpcAttrsGetter]{
		Getter: getter,
	}

	request := testRpcRequest{
		system:        "grpc",
		service:       "/helloworld.Greeter",
		method:        "SayHello",
		serverAddress: "localhost:50051",
	}

	attrs := []attribute.KeyValue{}
	ctx := context.Background()

	attrs, newCtx := extractor.OnStart(ctx, attrs, request)

	require.NotNil(t, newCtx)
	require.Len(t, attrs, 4)

	expectedAttrs := map[attribute.Key]attribute.Value{
		semconv.RPCSystemKey:     attribute.StringValue("grpc"),
		semconv.RPCServiceKey:    attribute.StringValue("/helloworld.Greeter"),
		semconv.RPCMethodKey:     attribute.StringValue("SayHello"),
		semconv.ServerAddressKey: attribute.StringValue("localhost:50051"),
	}

	for _, attr := range attrs {
		expectedValue, ok := expectedAttrs[attr.Key]
		require.True(t, ok, "unexpected attribute: %s", attr.Key)
		require.Equal(t, expectedValue, attr.Value, "attribute %s has wrong value", attr.Key)
	}
}

func TestRpcAttrsExtractor_OnEnd_Success(t *testing.T) {
	getter := testRpcAttrsGetter{}
	extractor := &RpcAttrsExtractor[testRpcRequest, any, testRpcAttrsGetter]{
		Getter: getter,
	}

	request := testRpcRequest{
		system:        "grpc",
		service:       "/helloworld.Greeter",
		method:        "SayHello",
		serverAddress: "localhost:50051",
	}

	attrs := []attribute.KeyValue{}
	ctx := context.Background()

	attrs, newCtx := extractor.OnEnd(ctx, attrs, request, nil, nil)

	require.NotNil(t, newCtx)
	require.Len(t, attrs, 1)
	require.Equal(t, semconv.RPCGRPCStatusCodeKey, attrs[0].Key)
	require.Equal(t, int64(0), attrs[0].Value.AsInt64())
}

func TestRpcAttrsExtractor_OnEnd_Error(t *testing.T) {
	getter := testRpcAttrsGetter{}
	extractor := &RpcAttrsExtractor[testRpcRequest, any, testRpcAttrsGetter]{
		Getter: getter,
	}

	request := testRpcRequest{
		system:        "grpc",
		service:       "/helloworld.Greeter",
		method:        "SayHello",
		serverAddress: "localhost:50051",
	}

	attrs := []attribute.KeyValue{}
	ctx := context.Background()
	testErr := errors.New("test error")

	attrs, newCtx := extractor.OnEnd(ctx, attrs, request, nil, testErr)

	require.NotNil(t, newCtx)
	require.Len(t, attrs, 1)
	require.Equal(t, semconv.RPCGRPCStatusCodeKey, attrs[0].Key)
	require.Equal(t, int64(2), attrs[0].Value.AsInt64()) // gRPC status code 2 = UNKNOWN
}

func TestRpcAttrsExtractor_OnEnd_NonGrpc(t *testing.T) {
	getter := testRpcAttrsGetter{}
	extractor := &RpcAttrsExtractor[testRpcRequest, any, testRpcAttrsGetter]{
		Getter: getter,
	}

	request := testRpcRequest{
		system:        "other_rpc",
		service:       "/helloworld.Greeter",
		method:        "SayHello",
		serverAddress: "localhost:50051",
	}

	attrs := []attribute.KeyValue{}
	ctx := context.Background()

	attrs, newCtx := extractor.OnEnd(ctx, attrs, request, nil, nil)

	require.NotNil(t, newCtx)
	require.Len(t, attrs, 0, "non-grpc systems should not add status code")
}

func TestServerRpcAttrsExtractor(t *testing.T) {
	getter := testRpcAttrsGetter{}
	baseExtractor := RpcAttrsExtractor[testRpcRequest, any, testRpcAttrsGetter]{
		Getter: getter,
	}
	extractor := &ServerRpcAttrsExtractor[testRpcRequest, any, testRpcAttrsGetter]{
		Base: baseExtractor,
	}

	request := testRpcRequest{
		system:        "grpc",
		service:       "/helloworld.Greeter",
		method:        "SayHello",
		serverAddress: "localhost:50051",
	}

	// Test OnStart
	attrs := []attribute.KeyValue{}
	ctx := context.Background()
	attrs, ctx = extractor.OnStart(ctx, attrs, request)
	require.Len(t, attrs, 4)

	// Test OnEnd
	attrs = []attribute.KeyValue{}
	attrs, ctx = extractor.OnEnd(ctx, attrs, request, nil, nil)
	require.Len(t, attrs, 1)

	// Test span key
	key := extractor.GetSpanKey()
	require.Equal(t, "opentelemetry-traces-span-key-rpc-server", string(key))
}

func TestClientRpcAttrsExtractor(t *testing.T) {
	getter := testRpcAttrsGetter{}
	baseExtractor := RpcAttrsExtractor[testRpcRequest, any, testRpcAttrsGetter]{
		Getter: getter,
	}
	extractor := &ClientRpcAttrsExtractor[testRpcRequest, any, testRpcAttrsGetter]{
		Base: baseExtractor,
	}

	request := testRpcRequest{
		system:        "grpc",
		service:       "/helloworld.Greeter",
		method:        "SayHello",
		serverAddress: "localhost:50051",
	}

	// Test OnStart
	attrs := []attribute.KeyValue{}
	ctx := context.Background()
	attrs, ctx = extractor.OnStart(ctx, attrs, request)
	require.Len(t, attrs, 4)

	// Test OnEnd
	attrs = []attribute.KeyValue{}
	attrs, ctx = extractor.OnEnd(ctx, attrs, request, nil, nil)
	require.Len(t, attrs, 1)

	// Test span key
	key := extractor.GetSpanKey()
	require.Equal(t, "opentelemetry-traces-span-key-rpc-client", string(key))
}
