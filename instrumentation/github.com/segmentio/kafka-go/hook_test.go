// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package kafka

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

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/hook/hooktest"
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

func TestReadMessage_LinksToProducerAndSetsAttrs(t *testing.T) {
	sr := setupTest(t)

	// Simulate the producer having injected a trace context into the message.
	tid, err := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	require.NoError(t, err)
	sid, err := trace.SpanIDFromHex("0102030405060708")
	require.NoError(t, err)
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    tid,
		SpanID:     sid,
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	producerCtx := trace.ContextWithSpanContext(context.Background(), sc)

	var headers []kafka.Header
	propagator.Inject(producerCtx, headerCarrier{headers: &headers})

	msg := kafka.Message{
		Topic:     "orders",
		Partition: 3,
		Offset:    42,
		Key:       []byte("k1"),
		Value:     []byte("hello"),
		Headers:   headers,
	}

	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers: []string{"localhost:9092"},
		Topic:   "orders",
	})
	t.Cleanup(func() { _ = r.Close() })

	ictx := hooktest.NewMockHookContext(r, context.Background())
	BeforeReadMessage(ictx, r, context.Background())
	AfterReadMessage(ictx, msg, nil)

	spans := sr.Ended()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, "orders receive", span.Name())
	assert.Equal(t, trace.SpanKindConsumer, span.SpanKind())
	// The consumer span must be part of the producer's trace.
	assert.Equal(t, tid, span.SpanContext().TraceID())
	assert.Equal(t, tid, span.Parent().TraceID())
	assert.Equal(t, sid, span.Parent().SpanID())

	m := spanAttrs(span)
	assert.Equal(t, "kafka", m["messaging.system"])
	assert.Equal(t, "receive", m["messaging.operation.name"])
	assert.Equal(t, "orders", m["messaging.destination.name"])
	assert.Equal(t, "localhost", m["server.address"])
	assert.Equal(t, "3", m["messaging.destination.partition.id"])
	assert.Equal(t, int64(42), m["messaging.kafka.offset"])
}

func TestReadMessage_RecordsError(t *testing.T) {
	sr := setupTest(t)

	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers: []string{"localhost:9092"},
		Topic:   "orders",
	})
	t.Cleanup(func() { _ = r.Close() })

	ictx := hooktest.NewMockHookContext(r, context.Background())
	BeforeReadMessage(ictx, r, context.Background())
	AfterReadMessage(ictx, kafka.Message{}, errors.New("read timeout"))

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status().Code)
	assert.Contains(t, spans[0].Status().Description, "read timeout")

	// On error there is no valid partition/offset, so those attrs are omitted.
	m := spanAttrs(spans[0])
	_, hasPartition := m["messaging.destination.partition.id"]
	assert.False(t, hasPartition)
	_, hasOffset := m["messaging.kafka.offset"]
	assert.False(t, hasOffset)
}

func TestReadMessage_Disabled(t *testing.T) {
	sr := setupTest(t)
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "kafka")

	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers: []string{"localhost:9092"},
		Topic:   "orders",
	})
	t.Cleanup(func() { _ = r.Close() })

	ictx := hooktest.NewMockHookContext(r, context.Background())
	BeforeReadMessage(ictx, r, context.Background())
	AfterReadMessage(ictx, kafka.Message{Topic: "orders"}, nil)

	assert.Empty(t, sr.Ended())
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
