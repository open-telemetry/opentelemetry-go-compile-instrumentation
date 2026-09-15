// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package propagation

import (
	"testing"

	"github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func newSpanContext(t *testing.T, traceID, spanID string) trace.SpanContext {
	t.Helper()

	tid, err := trace.TraceIDFromHex(traceID)
	require.NoError(t, err)
	sid, err := trace.SpanIDFromHex(spanID)
	require.NoError(t, err)

	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    tid,
		SpanID:     sid,
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
}

func TestHeaderCarrier_Get(t *testing.T) {
	headers := []kafka.Header{
		{Key: "traceparent", Value: []byte("first")},
		{Key: "traceparent", Value: []byte("second")},
		{Key: "baggage", Value: []byte("bg")},
	}
	c := NewHeaderCarrier(&headers)

	// Kafka allows repeated keys; the first one wins.
	assert.Equal(t, "first", c.Get("traceparent"))
	assert.Equal(t, "bg", c.Get("baggage"))
	assert.Empty(t, c.Get("absent"))

	// Keys are compared byte-for-byte, not canonicalized as in net/http.
	assert.Empty(t, c.Get("Traceparent"))
}

func TestHeaderCarrier_SetReplacesInPlace(t *testing.T) {
	var headers []kafka.Header
	c := NewHeaderCarrier(&headers)

	c.Set("traceparent", "tp-1")
	c.Set("baggage", "bg-1")
	require.Len(t, headers, 2)

	// Appending a second traceparent instead of overwriting would leave Get
	// returning the stale "tp-1", since it resolves to the first match.
	c.Set("traceparent", "tp-2")

	assert.Len(t, headers, 2)
	assert.Equal(t, "tp-2", c.Get("traceparent"))
	assert.Equal(t, "traceparent", headers[0].Key)
	assert.Equal(t, "bg-1", c.Get("baggage"))
}

func TestHeaderCarrier_SetGetKeys(t *testing.T) {
	var headers []kafka.Header
	hc := NewHeaderCarrier(&headers)

	hc.Set("traceparent", "v1")
	hc.Set("baggage", "v2")
	assert.Equal(t, "v1", hc.Get("traceparent"))
	assert.Equal(t, "v2", hc.Get("baggage"))
	assert.Empty(t, hc.Get("absent"))

	// Set on an existing key overwrites rather than appending a duplicate.
	hc.Set("traceparent", "v3")
	assert.Equal(t, "v3", hc.Get("traceparent"))
	assert.Len(t, headers, 2)

	assert.ElementsMatch(t, []string{"traceparent", "baggage"}, hc.Keys())
}

func TestHeaderCarrier_Keys(t *testing.T) {
	headers := []kafka.Header{
		{Key: "traceparent", Value: []byte("tp")},
		{Key: "tracestate", Value: []byte("ts")},
		{Key: "baggage", Value: []byte("bg")},
	}

	assert.Equal(
		t,
		[]string{"traceparent", "tracestate", "baggage"},
		NewHeaderCarrier(&headers).Keys(),
	)
}

// Kafka permits repeated keys, and Keys reports them verbatim rather than
// deduplicating. Get and Set both resolve to the first match, so collapsing
// duplicates here would hide a header that Set will never reach and that a
// broker will still deliver.
func TestHeaderCarrier_KeysReportsDuplicates(t *testing.T) {
	headers := []kafka.Header{
		{Key: "traceparent", Value: []byte("first")},
		{Key: "baggage", Value: []byte("bg")},
		{Key: "traceparent", Value: []byte("second")},
	}
	c := NewHeaderCarrier(&headers)

	assert.Equal(t, []string{"traceparent", "baggage", "traceparent"}, c.Keys())

	// Set rewrites only the first duplicate, so the key count is unchanged and
	// the trailing "second" survives untouched.
	c.Set("traceparent", "third")

	assert.Equal(t, []string{"traceparent", "baggage", "traceparent"}, c.Keys())
	assert.Equal(t, "third", c.Get("traceparent"))
	assert.Equal(t, "second", string(headers[2].Value))
}

func TestHeaderCarrier_MessageWithoutHeaders(t *testing.T) {
	msg := kafka.Message{}
	c := NewHeaderCarrier(&msg.Headers)

	require.Nil(t, msg.Headers)
	assert.Empty(t, c.Keys())
	assert.Empty(t, c.Get("traceparent"))

	c.Set("traceparent", "tp-1")

	assert.Equal(t, "tp-1", c.Get("traceparent"))
	require.Len(t, msg.Headers, 1)
	assert.Equal(t, "traceparent", msg.Headers[0].Key)
}

func TestHeaderCarrier_PropagatorRoundTrip(t *testing.T) {
	prop := propagation.TraceContext{}
	want := newSpanContext(t, "0102030405060708090a0b0c0d0e0f10", "0102030405060708")

	var headers []kafka.Header
	prop.Inject(trace.ContextWithSpanContext(t.Context(), want), NewHeaderCarrier(&headers))

	require.Len(t, headers, 1)
	assert.Equal(t, "traceparent", headers[0].Key)

	msg := kafka.Message{Headers: headers}
	got := trace.SpanContextFromContext(prop.Extract(t.Context(), NewHeaderCarrier(&msg.Headers)))

	assert.Equal(t, want.TraceID(), got.TraceID())
	assert.Equal(t, want.SpanID(), got.SpanID())
	assert.True(t, got.IsSampled())
	assert.True(t, got.IsRemote())
}

// Re-injecting into an already-injected message must replace the traceparent.
// A duplicate would extract the stale context, attaching downstream spans to
// the wrong trace.
func TestHeaderCarrier_ReinjectReplacesTraceparent(t *testing.T) {
	prop := propagation.TraceContext{}
	stale := newSpanContext(t, "0102030405060708090a0b0c0d0e0f10", "0102030405060708")
	fresh := newSpanContext(t, "1112131415161718191a1b1c1d1e1f20", "1112131415161718")

	var headers []kafka.Header
	c := NewHeaderCarrier(&headers)
	prop.Inject(trace.ContextWithSpanContext(t.Context(), stale), c)
	prop.Inject(trace.ContextWithSpanContext(t.Context(), fresh), c)

	require.Len(t, headers, 1)

	got := trace.SpanContextFromContext(prop.Extract(t.Context(), c))

	assert.Equal(t, fresh.TraceID(), got.TraceID())
	assert.Equal(t, fresh.SpanID(), got.SpanID())
	assert.NotEqual(t, stale.TraceID(), got.TraceID())
}
