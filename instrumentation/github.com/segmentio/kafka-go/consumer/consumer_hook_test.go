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

	kafkaprop "go.opentelemetry.io/otelc/instrumentation/github.com/segmentio/kafka-go/internal/propagation"
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
	propagator.Inject(producerCtx, kafkaprop.NewHeaderCarrier(&headers))

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

func TestReadMessage_InvalidUTF8MessageKey(t *testing.T) {
	sr := setupTest(t)

	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers: []string{"localhost:9092"},
		Topic:   "orders",
	})
	t.Cleanup(func() { _ = r.Close() })

	msg := kafka.Message{
		Topic:     "orders",
		Partition: 3,
		Offset:    42,
		Key:       []byte{'o', 0xff, 'k'},
		Value:     []byte("hello"),
	}

	ictx := hooktest.NewMockHookContext(r, context.Background())
	BeforeReadMessage(ictx, r, context.Background())
	AfterReadMessage(ictx, msg, nil)

	spans := sr.Ended()
	require.Len(t, spans, 1)

	m := spanAttrs(spans[0])
	assert.Equal(t, "o\uFFFDk", m["messaging.kafka.message.key"])
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
	propagator.Inject(producerCtx, kafkaprop.NewHeaderCarrier(&headers))

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

// -----------------------------------------------------------------------------
// FetchMessage tests
// -----------------------------------------------------------------------------

func TestFetchMessage_LinksToProducerAndSetsAttrs(t *testing.T) {
	sr := setupTest(t)

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
	propagator.Inject(producerCtx, kafkaprop.NewHeaderCarrier(&headers))

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
	BeforeFetchMessage(ictx, r, context.Background())
	AfterFetchMessage(ictx, msg, nil)

	spans := sr.Ended()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, "orders receive", span.Name())
	assert.Equal(t, trace.SpanKindConsumer, span.SpanKind())
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

func TestFetchMessage_RecordsError(t *testing.T) {
	sr := setupTest(t)

	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers: []string{"localhost:9092"},
		Topic:   "orders",
	})
	t.Cleanup(func() { _ = r.Close() })

	ictx := hooktest.NewMockHookContext(r, context.Background())
	BeforeFetchMessage(ictx, r, context.Background())
	AfterFetchMessage(ictx, kafka.Message{}, errors.New("fetch timeout"))

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status().Code)
	assert.Contains(t, spans[0].Status().Description, "fetch timeout")

	m := spanAttrs(spans[0])
	_, hasPartition := m["messaging.destination.partition.id"]
	assert.False(t, hasPartition)
	_, hasOffset := m["messaging.kafka.offset"]
	assert.False(t, hasOffset)
}

func TestFetchMessage_Disabled(t *testing.T) {
	sr := setupTest(t)
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "kafka")

	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers: []string{"localhost:9092"},
		Topic:   "orders",
	})
	t.Cleanup(func() { _ = r.Close() })

	ictx := hooktest.NewMockHookContext(r, context.Background())
	BeforeFetchMessage(ictx, r, context.Background())
	AfterFetchMessage(ictx, kafka.Message{Topic: "orders"}, nil)

	assert.Empty(t, sr.Ended())
}

// TestReadMessage_NestedFetchMessageDoesNotDuplicateSpan is a regression test
// for https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/1076:
// (*kafka.Reader).ReadMessage's own implementation calls r.FetchMessage(ctx)
// internally, and since both methods are instrumented, that nested call used to
// trigger BeforeFetchMessage/AfterFetchMessage as well, producing two consumer
// spans for what is, from the caller's point of view, a single ReadMessage call.
//
// This drives the hooks the way otelc's generated trampolines actually would:
// BeforeReadMessage's marked ctx (read back via GetParam, mirroring how the real
// ReadMessage body picks up the mutated parameter) is what the nested
// FetchMessage call — with its own, separate HookContext — receives.
func TestReadMessage_NestedFetchMessageDoesNotDuplicateSpan(t *testing.T) {
	sr := setupTest(t)

	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers: []string{"localhost:9092"},
		Topic:   "orders",
	})
	t.Cleanup(func() { _ = r.Close() })

	msg := kafka.Message{Topic: "orders", Partition: 1, Offset: 7}

	readCtx := hooktest.NewMockHookContext(r, context.Background())
	BeforeReadMessage(readCtx, r, context.Background())

	// ReadMessage's own body would now call r.FetchMessage using the ctx
	// BeforeReadMessage wrote back via SetParam(1, ...).
	nestedCtx, ok := readCtx.GetParam(1).(context.Context)
	require.True(t, ok, "BeforeReadMessage should write the marked ctx back via SetParam")

	fetchIctx := hooktest.NewMockHookContext(r, nestedCtx)
	BeforeFetchMessage(fetchIctx, r, nestedCtx)
	AfterFetchMessage(fetchIctx, msg, nil)

	// The nested FetchMessage call must not have created a span of its own.
	assert.Empty(t, sr.Ended(), "nested FetchMessage call should not create a duplicate span")

	AfterReadMessage(readCtx, msg, nil)

	// Only the outer ReadMessage call produces a span.
	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, "orders receive", spans[0].Name())
}

// TestFetchMessage_DirectCallIsUnaffectedByNestedCallDetection ensures a
// top-level FetchMessage call (not nested inside ReadMessage) still gets its
// own span: the nested-call marker is only present on a ctx that
// BeforeReadMessage produced.
func TestFetchMessage_DirectCallIsUnaffectedByNestedCallDetection(t *testing.T) {
	sr := setupTest(t)

	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers: []string{"localhost:9092"},
		Topic:   "orders",
	})
	t.Cleanup(func() { _ = r.Close() })

	ictx := hooktest.NewMockHookContext(r, context.Background())
	BeforeFetchMessage(ictx, r, context.Background())
	AfterFetchMessage(ictx, kafka.Message{Topic: "orders"}, nil)

	require.Len(t, sr.Ended(), 1)
}
