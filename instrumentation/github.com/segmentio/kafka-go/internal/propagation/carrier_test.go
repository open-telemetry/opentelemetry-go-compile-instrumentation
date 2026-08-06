// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package propagation

import (
	"context"
	"testing"

	"github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func TestHeaderCarrier_Get(t *testing.T) {
	headers := []kafka.Header{
		{Key: "key1", Value: []byte("value1")},
		{Key: "key2", Value: []byte("value2")},
	}
	carrier := NewHeaderCarrier(&headers)

	assert.Equal(t, "value1", carrier.Get("key1"))
	assert.Equal(t, "value2", carrier.Get("key2"))
	assert.Equal(t, "", carrier.Get("nonexistent"))
}

func TestHeaderCarrier_Set_ReplaceExisting(t *testing.T) {
	headers := []kafka.Header{
		{Key: "key1", Value: []byte("value1")},
	}
	carrier := NewHeaderCarrier(&headers)

	carrier.Set("key1", "newvalue")

	assert.Len(t, headers, 1)
	assert.Equal(t, "key1", headers[0].Key)
	assert.Equal(t, []byte("newvalue"), headers[0].Value)
}

func TestHeaderCarrier_Set_AppendNew(t *testing.T) {
	headers := []kafka.Header{
		{Key: "key1", Value: []byte("value1")},
	}
	carrier := NewHeaderCarrier(&headers)

	carrier.Set("key2", "value2")

	assert.Len(t, headers, 2)
	assert.Equal(t, "key2", headers[1].Key)
	assert.Equal(t, []byte("value2"), headers[1].Value)
}

func TestHeaderCarrier_Keys(t *testing.T) {
	headers := []kafka.Header{
		{Key: "key1", Value: []byte("value1")},
		{Key: "key2", Value: []byte("value2")},
		{Key: "key3", Value: []byte("value3")},
	}
	carrier := NewHeaderCarrier(&headers)

	keys := carrier.Keys()

	assert.Len(t, keys, 3)
	assert.Contains(t, keys, "key1")
	assert.Contains(t, keys, "key2")
	assert.Contains(t, keys, "key3")
}

func TestHeaderCarrier_Keys_Empty(t *testing.T) {
	headers := []kafka.Header{}
	carrier := NewHeaderCarrier(&headers)

	keys := carrier.Keys()

	assert.Len(t, keys, 0)
}

func TestHeaderCarrier_DuplicateKeys(t *testing.T) {
	headers := []kafka.Header{
		{Key: "key1", Value: []byte("first")},
		{Key: "key1", Value: []byte("second")},
	}
	carrier := NewHeaderCarrier(&headers)

	// Get returns first matching key
	assert.Equal(t, "first", carrier.Get("key1"))

	// Keys includes duplicate entries
	keys := carrier.Keys()
	assert.Equal(t, []string{"key1", "key1"}, keys)

	// Set overwrites only the first match
	carrier.Set("key1", "overwritten")
	assert.Len(t, headers, 2)
	assert.Equal(t, "overwritten", string(headers[0].Value))
	assert.Equal(t, "second", string(headers[1].Value))
}

func TestHeaderCarrier_NilSlice(t *testing.T) {
	var headers []kafka.Header
	carrier := NewHeaderCarrier(&headers)

	assert.Equal(t, "", carrier.Get("key1"))
	assert.Empty(t, carrier.Keys())

	carrier.Set("key1", "value1")
	assert.Len(t, headers, 1)
	assert.Equal(t, "key1", headers[0].Key)
	assert.Equal(t, []byte("value1"), headers[0].Value)
}

func TestHeaderCarrier_Propagator_RoundTrip(t *testing.T) {
	tc := propagation.TraceContext{}
	headers := []kafka.Header{}
	carrier := NewHeaderCarrier(&headers)

	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:     trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8},
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithRemoteSpanContext(context.Background(), sc)

	// Inject trace context into carrier
	tc.Inject(ctx, carrier)
	assert.NotEmpty(t, headers)

	// Extract trace context back from carrier
	extractedCtx := tc.Extract(context.Background(), carrier)
	extractedSC := trace.SpanContextFromContext(extractedCtx)

	assert.True(t, extractedSC.IsValid())
	assert.Equal(t, sc.TraceID(), extractedSC.TraceID())
	assert.Equal(t, sc.SpanID(), extractedSC.SpanID())
	assert.Equal(t, sc.TraceFlags(), extractedSC.TraceFlags())
}
