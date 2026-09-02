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
		hc := kafkaprop.NewHeaderCarrier(&msgs[i].Headers)
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

func TestBeforeWriteMessages_InvalidUTF8MessageKey(t *testing.T) {
	sr := setupTest(t)

	w := &kafka.Writer{Addr: kafka.TCP("localhost:9092"), Topic: "orders"}
	msgs := []kafka.Message{{Key: []byte{'o', 0xff, 'k'}, Value: []byte("hello")}}

	ictx := hooktest.NewMockHookContext(w, context.Background(), msgs)
	BeforeWriteMessages(ictx, w, context.Background(), msgs...)
	AfterWriteMessages(ictx, nil)

	spans := sr.Ended()
	require.Len(t, spans, 1)

	m := spanAttrs(spans[0])
	assert.Equal(t, "o\uFFFDk", m["messaging.kafka.message.key"])
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

// TestBeforeWriteMessages_AsyncWriterMarksSpans is a regression test for
// https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/issues/1177:
// for a kafka.Writer with Async enabled, WriteMessages returns before the
// broker write happens, so span duration and status here only reflect local
// hand-off, not delivery. Spans for such writes must carry the
// messaging.kafka.async attribute so that isn't misread as confirmed delivery.
func TestBeforeWriteMessages_AsyncWriterMarksSpans(t *testing.T) {
	sr := setupTest(t)

	w := &kafka.Writer{Addr: kafka.TCP("localhost:9092"), Topic: "orders", Async: true}
	msgs := []kafka.Message{{Key: []byte("k1"), Value: []byte("hello")}}

	ictx := hooktest.NewMockHookContext(w, context.Background(), msgs)
	BeforeWriteMessages(ictx, w, context.Background(), msgs...)
	// Async writers return nil from WriteMessages immediately, regardless of
	// what eventually happens to the write.
	AfterWriteMessages(ictx, nil)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	m := spanAttrs(spans[0])
	assert.Equal(t, true, m["messaging.kafka.async"])
}

// TestBeforeWriteMessages_SyncWriterOmitsAsyncAttr guards against the async
// marker leaking onto spans for the default, synchronous writer, where
// WriteMessages does block until the broker write completes and span
// duration/status are already accurate.
func TestBeforeWriteMessages_SyncWriterOmitsAsyncAttr(t *testing.T) {
	sr := setupTest(t)

	w := &kafka.Writer{Addr: kafka.TCP("localhost:9092"), Topic: "orders"}
	msgs := []kafka.Message{{Key: []byte("k1"), Value: []byte("hello")}}

	ictx := hooktest.NewMockHookContext(w, context.Background(), msgs)
	BeforeWriteMessages(ictx, w, context.Background(), msgs...)
	AfterWriteMessages(ictx, nil)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	m := spanAttrs(spans[0])
	_, hasAsync := m["messaging.kafka.async"]
	assert.False(t, hasAsync)
}

// TestEnsureAsyncFailureLogging_ChainsOriginalCompletion verifies that any
// Completion callback the caller already configured still runs when a failure
// occurs, and receives the exact error and message slice.
func TestEnsureAsyncFailureLogging_ChainsOriginalCompletion(t *testing.T) {
	setupTest(t)

	var originalCalled bool
	var originalErr error
	var originalMsgs []kafka.Message
	w := &kafka.Writer{
		Addr:  kafka.TCP("localhost:9092"),
		Topic: "orders",
		Async: true,
		Completion: func(msgs []kafka.Message, err error) {
			originalCalled = true
			originalErr = err
			originalMsgs = msgs
		},
	}
	msgs := []kafka.Message{{Key: []byte("k1"), Value: []byte("hello")}}

	ictx := hooktest.NewMockHookContext(w, context.Background(), msgs)
	BeforeWriteMessages(ictx, w, context.Background(), msgs...)

	// Simulate kafka-go invoking Completion asynchronously, well after
	// WriteMessages (and AfterWriteMessages) already returned.
	writeErr := errors.New("leader not available")
	w.Completion(msgs, writeErr)

	assert.True(t, originalCalled, "the caller's own Completion callback must still be invoked")
	assert.Equal(t, writeErr, originalErr, "the caller's callback must see the real error")
	assert.Equal(t, msgs, originalMsgs, "the caller's callback must receive the original message slice")

	// Also verify that a writer with no original Completion does not panic when invoked.
	wNil := &kafka.Writer{Addr: kafka.TCP("localhost:9092"), Topic: "orders", Async: true}
	ictxNil := hooktest.NewMockHookContext(wNil, context.Background(), msgs)
	BeforeWriteMessages(ictxNil, wNil, context.Background(), msgs...)
	require.NotNil(t, wNil.Completion)
	assert.NotPanics(t, func() {
		wNil.Completion(msgs, writeErr)
	})
}

// TestEnsureAsyncFailureLogging_WrapsOnce verifies a *kafka.Writer reused
// across multiple WriteMessages calls only has its Completion wrapped once,
// so the completion callback is not nested or invoked multiple times.
func TestEnsureAsyncFailureLogging_WrapsOnce(t *testing.T) {
	setupTest(t)

	var callCount int
	w := &kafka.Writer{
		Addr:  kafka.TCP("localhost:9092"),
		Topic: "orders",
		Async: true,
		Completion: func(msgs []kafka.Message, err error) {
			callCount++
		},
	}
	msgs := []kafka.Message{{Key: []byte("k1"), Value: []byte("hello")}}

	for range 3 {
		ictx := hooktest.NewMockHookContext(w, context.Background(), msgs)
		BeforeWriteMessages(ictx, w, context.Background(), msgs...)
	}

	w.Completion(msgs, errors.New("leader not available"))
	assert.Equal(t, 1, callCount, "Completion must be wrapped exactly once regardless of call count")
}

// TestEnsureAsyncFailureLogging_ConcurrentCallsAreSafe is a regression test:
// ensureAsyncFailureLogging must be safe to call concurrently for the same
// writer, since kafka-go documents *kafka.Writer as safe to share across
// goroutines. Run with -race to catch a reintroduction of the data race where
// a losing caller's own WriteMessages call could read w.Completion
// concurrently with the winning caller still writing it.
func TestEnsureAsyncFailureLogging_ConcurrentCallsAreSafe(t *testing.T) {
	setupTest(t)

	w := &kafka.Writer{Addr: kafka.TCP("localhost:9092"), Topic: "orders", Async: true}
	msgs := []kafka.Message{{Key: []byte("k1"), Value: []byte("hello")}}

	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			ictx := hooktest.NewMockHookContext(w, context.Background(), msgs)
			BeforeWriteMessages(ictx, w, context.Background(), msgs...)
			// Simulate kafka-go's own background goroutine reading w.Completion
			// concurrently with other callers still wrapping it.
			w.Completion(msgs, nil)
		})
	}
	wg.Wait()
}
