// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package consumer

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

// TestExtractContext verifies that ExtractContext correctly extracts the trace
// context from a Kafka message's headers and returns a context that carries the
// propagated span context.
func TestExtractContext(t *testing.T) {
	setupTest(t)

	// Create a span context and inject it into message headers, simulating a
	// producer that has propagated its trace context.
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
		Topic:   "orders",
		Key:     []byte("k1"),
		Value:   []byte("hello"),
		Headers: headers,
	}

	// Extract the context from the message.
	extractedCtx := ExtractContext(msg)
	extractedSc := trace.SpanContextFromContext(extractedCtx)

	// Verify the extracted context matches what was injected.
	assert.True(t, extractedSc.IsValid())
	assert.Equal(t, tid, extractedSc.TraceID())
	assert.Equal(t, sid, extractedSc.SpanID())
	assert.True(t, extractedSc.IsSampled())
	assert.True(t, extractedSc.IsRemote())
}
