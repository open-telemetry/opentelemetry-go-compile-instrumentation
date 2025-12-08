// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/propagation"
	"google.golang.org/grpc/metadata"
)

func TestMetadataSupplier(t *testing.T) {
	md := metadata.MD{}
	supplier := NewMetadataSupplier(&md)

	// Test Set and Get
	supplier.Set("key1", "value1")
	assert.Equal(t, "value1", supplier.Get("key1"))

	// Test Get non-existent key
	assert.Empty(t, supplier.Get("non-existent"))

	// Test Keys
	supplier.Set("key2", "value2")
	keys := supplier.Keys()
	assert.Len(t, keys, 2)
	assert.Contains(t, keys, "key1")
	assert.Contains(t, keys, "key2")
}

func TestInject(t *testing.T) {
	propagator := propagation.TraceContext{}
	ctx := context.Background()

	// Create a context with trace info
	// Note: In a real scenario, you'd have an active span
	ctx = Inject(ctx, propagator)

	// Verify metadata was added to context
	md, ok := metadata.FromOutgoingContext(ctx)
	require.True(t, ok)
	require.NotNil(t, md)
}

func TestExtract(t *testing.T) {
	propagator := propagation.TraceContext{}

	// Create incoming context with metadata
	md := metadata.MD{
		"traceparent": []string{"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0aa902b7-01"},
	}
	ctx := metadata.NewIncomingContext(context.Background(), md)

	// Extract should work
	extractedCtx := Extract(ctx, propagator)
	require.NotNil(t, extractedCtx)
}
