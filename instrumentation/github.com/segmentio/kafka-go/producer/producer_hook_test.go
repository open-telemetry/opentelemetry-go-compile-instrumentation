// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package producer

import (
	"context"
	"errors"
	"sync"
	"testing"

	kafka "github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otelc/pkg/hook/hooktest"
)

// setupTest wires the package-level tracer/propagator to an in-memory span
// recorder, bypassing the real OTel SDK setup so hook behavior can be asserted
// deterministically. It also enables the kafka instrumentation for the test.
func setupTest(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "kafka")

	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))

	// Consume initOnce so initInstrumentation() becomes a no-op and does not
	// overwrite the tracer/propagator we install below.
	initOnce.Do(func() {})
	tracer = tp.Tracer("test")
	propagator = propagation.TraceContext{}

	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		initOnce = sync.Once{}
		tracer = nil
		propagator = nil
	})
	return sr
}

func spanAttrs(span sdktrace.ReadOnlySpan) map[string]interface{} {
	m := make(map[string]interface{})
	for _, a := range span.Attributes() {
		m[string(a.Key)] = a.Value.AsInterface()
	}
	return m
}

func TestBeforeWriteMessages_InjectsHeadersAndStartsSpans(t *testing.T) {
	sr := setupTest(t)

	w := &kafka.Writer{Addr: kafka.TCP("localhost:9092"), Topic: "orders"}
	msgs := []kafka.Message{
		{Key: []byte("k1"), Value: []byte("hello")},
		{Key: []byte("k2"), Value: []byte("world"), Topic: "override"},
	}

	ictx := hooktest.NewMockHookContext(w, context.Background(), msgs)
	BeforeWriteMessages(ictx, w, context.Background(), msgs...)

	// Each message must carry the propagated trace context.
	for i := range msgs {
		hc := headerCarrier{headers: &msgs[i].Headers}
		assert.NotEmpty(t, hc.Get("traceparent"), "message %d missing traceparent", i)
	}

	// The (header-injected) slice must be written back for the real call.
	written, ok := ictx.GetParam(2).([]kafka.Message)
	require.True(t, ok)
	require.Len(t, written, 2)

	AfterWriteMessages(ictx, nil)

	spans := sr.Ended()
	require.Len(t, spans, 2)

	assert.Equal(t, "orders send", spans[0].Name())
	assert.Equal(t, trace.SpanKindProducer, spans[0].SpanKind())
	// The second message overrides the topic, so its span name follows suit.
	assert.Equal(t, "override send", spans[1].Name())

	m := spanAttrs(spans[0])
	assert.Equal(t, "kafka", m["messaging.system"])
	assert.Equal(t, "send", m["messaging.operation.name"])
	assert.Equal(t, "orders", m["messaging.destination.name"])
	assert.Equal(t, "localhost", m["server.address"])
	assert.Equal(t, int64(9092), m["server.port"])
	assert.Equal(t, "k1", m["messaging.kafka.message.key"])
}

func TestAfterWriteMessages_RecordsError(t *testing.T) {
	sr := setupTest(t)

	w := &kafka.Writer{Addr: kafka.TCP("localhost:9092"), Topic: "orders"}
	msgs := []kafka.Message{{Value: []byte("hello")}}

	ictx := hooktest.NewMockHookContext(w, context.Background(), msgs)
	BeforeWriteMessages(ictx, w, context.Background(), msgs...)
	AfterWriteMessages(ictx, errors.New("broker unavailable"))

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status().Code)
	assert.Contains(t, spans[0].Status().Description, "broker unavailable")
}

func TestWriteMessages_Disabled(t *testing.T) {
	sr := setupTest(t)
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "kafka")

	w := &kafka.Writer{Addr: kafka.TCP("localhost:9092"), Topic: "orders"}
	msgs := []kafka.Message{{Value: []byte("hello")}}

	ictx := hooktest.NewMockHookContext(w, context.Background(), msgs)
	BeforeWriteMessages(ictx, w, context.Background(), msgs...)
	AfterWriteMessages(ictx, nil)

	assert.Empty(t, sr.Ended())
	assert.Nil(t, ictx.GetData())
}

func TestHeaderCarrier_SetGetKeys(t *testing.T) {
	var headers []kafka.Header
	hc := headerCarrier{headers: &headers}

	hc.Set("traceparent", "v1")
	hc.Set("baggage", "v2")
	assert.Equal(t, "v1", hc.Get("traceparent"))
	assert.Equal(t, "v2", hc.Get("baggage"))
	assert.Equal(t, "", hc.Get("absent"))

	// Set on an existing key overwrites rather than appending a duplicate.
	hc.Set("traceparent", "v3")
	assert.Equal(t, "v3", hc.Get("traceparent"))
	assert.Len(t, headers, 2)

	assert.ElementsMatch(t, []string{"traceparent", "baggage"}, hc.Keys())
}

// TestAfterWriteMessages_PartialFailure verifies that when WriteMessages returns
// kafka.WriteErrors (a []error aligned with the message slice), only the spans for
// messages that actually failed are marked as Error; the spans for successful
// messages stay Ok.
func TestAfterWriteMessages_PartialFailure(t *testing.T) {
	sr := setupTest(t)

	w := &kafka.Writer{Addr: kafka.TCP("localhost:9092"), Topic: "orders"}
	msgs := []kafka.Message{
		{Key: []byte("k1"), Value: []byte("hello")},
		{Key: []byte("k2"), Value: []byte("world")},
	}

	ictx := hooktest.NewMockHookContext(w, context.Background(), msgs)
	BeforeWriteMessages(ictx, w, context.Background(), msgs...)

	// Simulate a partial failure: the first message succeeds, the second fails.
	writeErrs := kafka.WriteErrors{nil, errors.New("write failed")}
	AfterWriteMessages(ictx, writeErrs)

	spans := sr.Ended()
	require.Len(t, spans, 2)

	// First span should not be marked as Error (message succeeded).
	assert.Equal(t, codes.Unset, spans[0].Status().Code)

	// Second span should be marked as Error.
	assert.Equal(t, codes.Error, spans[1].Status().Code)
	assert.Contains(t, spans[1].Status().Description, "write failed")
}

// TestAfterWriteMessages_AlwaysEndsSpans verifies that AfterWriteMessages ends
// spans even when instrumentation is disabled between Before and After calls.
// This prevents span leaks when the Enable() flag flips after headers have
// already been injected.
func TestAfterWriteMessages_AlwaysEndsSpans(t *testing.T) {
	sr := setupTest(t)

	w := &kafka.Writer{Addr: kafka.TCP("localhost:9092"), Topic: "orders"}
	msgs := []kafka.Message{
		{Key: []byte("k1"), Value: []byte("hello")},
		{Key: []byte("k2"), Value: []byte("world")},
	}

	// BeforeWriteMessages runs while kafka is enabled — spans are created and
	// trace headers are injected into the messages.
	ictx := hooktest.NewMockHookContext(w, context.Background(), msgs)
	BeforeWriteMessages(ictx, w, context.Background(), msgs...)

	// Simulate instrumentation being disabled between Before and After.
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "kafka")

	// AfterWriteMessages must still end the spans to avoid leaking them.
	AfterWriteMessages(ictx, nil)

	spans := sr.Ended()
	require.Len(t, spans, 2, "spans must be ended even when instrumentation is disabled after Before")
}
