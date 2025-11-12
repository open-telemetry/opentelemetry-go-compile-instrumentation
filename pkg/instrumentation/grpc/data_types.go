// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package grpc

import (
	"go.opentelemetry.io/otel/propagation"
	"google.golang.org/grpc/metadata"
)

// grpcRequest represents a gRPC request with extracted attributes
type grpcRequest struct {
	methodName    string
	serverAddress string
	propagators   propagation.TextMapCarrier
}

// grpcResponse represents a gRPC response with extracted attributes
type grpcResponse struct {
	statusCode int
}

// grpcMetadataCarrier implements propagation.TextMapCarrier for gRPC metadata
// This allows context propagation through gRPC metadata
type grpcMetadataCarrier struct {
	metadata *metadata.MD
}

// Get returns the value associated with the passed key
func (c *grpcMetadataCarrier) Get(key string) string {
	values := (*c.metadata).Get(key)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

// Set stores the key-value pair
func (c *grpcMetadataCarrier) Set(key, value string) {
	(*c.metadata).Set(key, value)
}

// Keys lists the keys stored in this carrier
func (c *grpcMetadataCarrier) Keys() []string {
	keys := make([]string, 0, len(*c.metadata))
	for k := range *c.metadata {
		keys = append(keys, k)
	}
	return keys
}

// gRPCContextKey is used to store gRPC context in the context
type gRPCContextKey struct{}

// gRPCContext stores gRPC-specific context information
type gRPCContext struct {
	methodName string
}
