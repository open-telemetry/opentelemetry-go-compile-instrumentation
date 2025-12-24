// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"context"

	"go.opentelemetry.io/otel/propagation"
	"google.golang.org/grpc/metadata"
)

// MetadataSupplier is a TextMapCarrier for gRPC metadata
type MetadataSupplier struct {
	metadata *metadata.MD
}

// NewMetadataSupplier creates a new MetadataSupplier
func NewMetadataSupplier(md *metadata.MD) MetadataSupplier {
	return MetadataSupplier{metadata: md}
}

// Get returns the value for a key from metadata
func (s MetadataSupplier) Get(key string) string {
	values := s.metadata.Get(key)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

// Set sets a key-value pair in metadata
func (s MetadataSupplier) Set(key, value string) {
	s.metadata.Set(key, value)
}

// Keys returns all keys in metadata
func (s MetadataSupplier) Keys() []string {
	out := make([]string, 0, len(*s.metadata))
	for key := range *s.metadata {
		out = append(out, key)
	}
	return out
}

// Inject injects the context into outgoing gRPC metadata
func Inject(ctx context.Context, propagators propagation.TextMapPropagator) context.Context {
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		md = metadata.MD{}
	}
	propagators.Inject(ctx, NewMetadataSupplier(&md))
	return metadata.NewOutgoingContext(ctx, md)
}

// Extract extracts the context from incoming gRPC metadata
func Extract(ctx context.Context, propagators propagation.TextMapPropagator) context.Context {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		md = metadata.MD{}
	}
	return propagators.Extract(ctx, NewMetadataSupplier(&md))
}
