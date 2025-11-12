// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rpc

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst-api-semconv/instrumenter/utils"
)

// RpcAttrsExtractor extracts RPC attributes from requests and responses
// following OpenTelemetry semantic conventions for RPC systems.
type RpcAttrsExtractor[REQUEST any, RESPONSE any, GETTER RpcAttrsGetter[REQUEST]] struct {
	Getter GETTER
}

// OnStart extracts attributes at the start of an RPC call
func (r *RpcAttrsExtractor[REQUEST, RESPONSE, GETTER]) OnStart(
	parentContext context.Context,
	attributes []attribute.KeyValue,
	request REQUEST,
) ([]attribute.KeyValue, context.Context) {
	attributes = append(attributes,
		attribute.KeyValue{
			Key:   semconv.RPCSystemKey,
			Value: attribute.StringValue(r.Getter.GetSystem(request)),
		},
		attribute.KeyValue{
			Key:   semconv.RPCServiceKey,
			Value: attribute.StringValue(r.Getter.GetService(request)),
		},
		attribute.KeyValue{
			Key:   semconv.RPCMethodKey,
			Value: attribute.StringValue(r.Getter.GetMethod(request)),
		},
		attribute.KeyValue{
			Key:   semconv.ServerAddressKey,
			Value: attribute.StringValue(r.Getter.GetServerAddress(request)),
		},
	)
	return attributes, parentContext
}

// OnEnd extracts attributes at the end of an RPC call
func (r *RpcAttrsExtractor[REQUEST, RESPONSE, GETTER]) OnEnd(
	ctx context.Context,
	attributes []attribute.KeyValue,
	request REQUEST,
	response RESPONSE,
	err error,
) ([]attribute.KeyValue, context.Context) {
	// Only add gRPC status code if the RPC system is gRPC
	system := r.Getter.GetSystem(request)
	if system == "grpc" {
		statusCode := getGrpcStatusCode(err)
		attributes = append(attributes, attribute.KeyValue{
			Key:   semconv.RPCGRPCStatusCodeKey,
			Value: attribute.IntValue(statusCode),
		})
	}
	return attributes, ctx
}

// getGrpcStatusCode extracts the gRPC status code from an error
func getGrpcStatusCode(err error) int {
	if err == nil {
		return 0 // OK
	}

	// Try to extract gRPC status code from error
	if st, ok := status.FromError(err); ok {
		return int(st.Code())
	}

	// Default to UNKNOWN (2) for non-gRPC errors
	return int(codes.Unknown)
}

// ServerRpcAttrsExtractor is a server-specific wrapper for RpcAttrsExtractor
type ServerRpcAttrsExtractor[REQUEST any, RESPONSE any, GETTER RpcAttrsGetter[REQUEST]] struct {
	Base RpcAttrsExtractor[REQUEST, RESPONSE, GETTER]
}

// GetSpanKey returns the span key for server RPC operations
func (s *ServerRpcAttrsExtractor[REQUEST, RESPONSE, GETTER]) GetSpanKey() attribute.Key {
	return utils.RPCServerKey
}

// OnStart delegates to the base extractor
func (s *ServerRpcAttrsExtractor[REQUEST, RESPONSE, GETTER]) OnStart(
	parentContext context.Context,
	attributes []attribute.KeyValue,
	request REQUEST,
) ([]attribute.KeyValue, context.Context) {
	return s.Base.OnStart(parentContext, attributes, request)
}

// OnEnd delegates to the base extractor
func (s *ServerRpcAttrsExtractor[REQUEST, RESPONSE, GETTER]) OnEnd(
	ctx context.Context,
	attributes []attribute.KeyValue,
	request REQUEST,
	response RESPONSE,
	err error,
) ([]attribute.KeyValue, context.Context) {
	return s.Base.OnEnd(ctx, attributes, request, response, err)
}

// ClientRpcAttrsExtractor is a client-specific wrapper for RpcAttrsExtractor
type ClientRpcAttrsExtractor[REQUEST any, RESPONSE any, GETTER RpcAttrsGetter[REQUEST]] struct {
	Base RpcAttrsExtractor[REQUEST, RESPONSE, GETTER]
}

// GetSpanKey returns the span key for client RPC operations
func (c *ClientRpcAttrsExtractor[REQUEST, RESPONSE, GETTER]) GetSpanKey() attribute.Key {
	return utils.RPCClientKey
}

// OnStart delegates to the base extractor
func (c *ClientRpcAttrsExtractor[REQUEST, RESPONSE, GETTER]) OnStart(
	parentContext context.Context,
	attributes []attribute.KeyValue,
	request REQUEST,
) ([]attribute.KeyValue, context.Context) {
	return c.Base.OnStart(parentContext, attributes, request)
}

// OnEnd delegates to the base extractor
func (c *ClientRpcAttrsExtractor[REQUEST, RESPONSE, GETTER]) OnEnd(
	ctx context.Context,
	attributes []attribute.KeyValue,
	request REQUEST,
	response RESPONSE,
	err error,
) ([]attribute.KeyValue, context.Context) {
	return c.Base.OnEnd(ctx, attributes, request, response, err)
}
