// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package sarama

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/IBM/sarama"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otelc/pkg/hook/hooktest"
)

// setupTest wires the package-level tracer/propagator to an in-memory span
// recorder, bypassing the real OTel SDK setup so hook behavior can be asserted
// deterministically. It also enables the sarama instrumentation for the test.
func setupTest(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "sarama")

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

func startedSpans(sr *tracetest.SpanRecorder) []sdktrace.ReadWriteSpan {
	return sr.Started()
}

func TestBeforeNewClient_AppendsInterceptors(t *testing.T) {
	conf := sarama.NewConfig()
	ictx := hooktest.NewMockHookContext([]string{"broker:9092"}, conf)

	BeforeNewClient(ictx, []string{"broker:9092"}, conf)

	require.Len(t, conf.Producer.Interceptors, 1)
	require.Len(t, conf.Consumer.Interceptors, 1)
	assert.IsType(t, &producerInterceptor{}, conf.Producer.Interceptors[0])
	assert.IsType(t, &consumerInterceptor{}, conf.Consumer.Interceptors[0])
}

func TestBeforeNewClient_Idempotent(t *testing.T) {
	conf := sarama.NewConfig()

	BeforeNewClient(hooktest.NewMockHookContext([]string{"broker:9092"}, conf), []string{"broker:9092"}, conf)
	BeforeNewClient(hooktest.NewMockHookContext([]string{"broker:9092"}, conf), []string{"broker:9092"}, conf)

	assert.Len(t, conf.Producer.Interceptors, 1, "second call must not append another interceptor")
	assert.Len(t, conf.Consumer.Interceptors, 1)
}

func TestBeforeNewClient_Disabled(t *testing.T) {
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "sarama")

	conf := sarama.NewConfig()
	BeforeNewClient(hooktest.NewMockHookContext([]string{"broker:9092"}, conf), []string{"broker:9092"}, conf)

	assert.Empty(t, conf.Producer.Interceptors)
	assert.Empty(t, conf.Consumer.Interceptors)

	ictx := hooktest.NewMockHookContext([]string{"broker:9092"}, (*sarama.Config)(nil))
	BeforeNewClient(ictx, []string{"broker:9092"}, nil)
	assert.Nil(t, ictx.GetParam(1), "disabled instrumentation must preserve nil config")
}

func TestBeforeNewClient_NilConfig(t *testing.T) {
	setupTest(t)
	ictx := hooktest.NewMockHookContext([]string{"broker:9092"}, (*sarama.Config)(nil))
	BeforeNewClient(ictx, []string{"broker:9092"}, nil)
	conf, ok := ictx.GetParam(1).(*sarama.Config)
	require.True(t, ok)
	require.NotNil(t, conf, "nil config must receive instrumented defaults")
	assert.Equal(t, sarama.NewConfig().Version, conf.Version)
	assert.Len(t, conf.Producer.Interceptors, 1)
	assert.Len(t, conf.Consumer.Interceptors, 1)
}

func TestProducerInterceptor_OnSend(t *testing.T) {
	sr := setupTest(t)

	msg := &sarama.ProducerMessage{
		Topic: "orders",
		Key:   sarama.StringEncoder("order-1"),
		Value: sarama.StringEncoder("hello"),
	}

	(&producerInterceptor{endpoint: "broker:9092", headersSupported: true}).OnSend(msg)

	spans := startedSpans(sr)
	require.Len(t, spans, 1)
	span := spans[0]

	assert.Equal(t, "orders send", span.Name())
	assert.Equal(t, trace.SpanKindProducer, span.SpanKind())

	attrs := spanAttrs(span)
	assert.Equal(t, "kafka", attrs["messaging.system"])
	assert.Equal(t, "send", attrs["messaging.operation.name"])
	assert.Equal(t, "orders", attrs["messaging.destination.name"])
	assert.Equal(t, true, attrs["messaging.kafka.async"])
	assert.Equal(t, "order-1", attrs["messaging.kafka.message.key"])
	assert.Equal(t, int64(5), attrs["messaging.message.body.size"])
	assert.Equal(t, "broker", attrs["server.address"])
	assert.Equal(t, int64(9092), attrs["server.port"])

	// The trace context must be injected into the message headers so an
	// instrumented consumer can continue the trace.
	tp := headerValue(t, msg.Headers, "traceparent")
	assert.True(t, strings.HasPrefix(tp, "00-"+span.SpanContext().TraceID().String()+"-"),
		"traceparent must carry the producer span's trace ID, got %q", tp)
}

func TestProducerInterceptor_OnSend_NilMessage(t *testing.T) {
	sr := setupTest(t)

	(&producerInterceptor{endpoint: "broker:9092"}).OnSend(nil)

	assert.Empty(t, startedSpans(sr))
}

func TestProducerInterceptor_OnSend_MetadataContext(t *testing.T) {
	sr := setupTest(t)

	_, parentSpan := tracer.Start(context.Background(), "caller",
		trace.WithSpanKind(trace.SpanKindInternal))
	defer parentSpan.End()
	ctx := trace.ContextWithSpan(context.Background(), parentSpan)

	msg := &sarama.ProducerMessage{
		Topic:    "orders",
		Value:    sarama.StringEncoder("hello"),
		Metadata: ctx,
	}

	(&producerInterceptor{endpoint: "broker:9092", headersSupported: true}).OnSend(msg)

	spans := startedSpans(sr)
	require.Len(t, spans, 2) // caller + producer
	for _, span := range spans {
		if span.Name() == "orders send" {
			assert.Equal(t, parentSpan.SpanContext().TraceID(), span.Parent().TraceID(),
				"producer span must be a child of the context carried in Metadata")
			assert.Equal(t, parentSpan.SpanContext().SpanID(), span.Parent().SpanID())
		}
	}
}

func TestProducerInterceptor_OnSend_EmptyTopic(t *testing.T) {
	sr := setupTest(t)

	msg := &sarama.ProducerMessage{Value: sarama.StringEncoder("hello")}
	(&producerInterceptor{endpoint: "broker:9092", headersSupported: true}).OnSend(msg)

	spans := startedSpans(sr)
	require.Len(t, spans, 1)
	assert.Equal(t, "send", spans[0].Name())
}

func TestProducerInterceptor_OnSend_OldProtocolSkipsInjection(t *testing.T) {
	sr := setupTest(t)

	msg := &sarama.ProducerMessage{
		Topic: "orders",
		Value: sarama.StringEncoder("hello"),
	}

	// headersSupported=false models Config.Version < V0_11_0_0: the span is
	// still created, but no record headers must be injected, because the
	// sarama dispatcher rejects messages with non-nil headers on old brokers.
	(&producerInterceptor{endpoint: "broker:9092", headersSupported: false}).OnSend(msg)

	spans := startedSpans(sr)
	require.Len(t, spans, 1)
	assert.Equal(t, "orders send", spans[0].Name())
	assert.Empty(t, msg.Headers, "no headers may be injected when the protocol cannot carry them")
}

func TestProducerInterceptor_OnSend_IdempotentRetry(t *testing.T) {
	sr := setupTest(t)

	msg := &sarama.ProducerMessage{
		Topic: "orders",
		Value: sarama.StringEncoder("hello"),
	}
	p := &producerInterceptor{endpoint: "broker:9092", headersSupported: true}

	// The dispatcher re-runs interceptors for retries of the same message.
	p.OnSend(msg)
	first := headerValue(t, msg.Headers, "traceparent")
	require.NotEmpty(t, first)

	p.OnSend(msg)

	// Exactly one span for both dispatches, and the injected trace context is
	// left untouched by the retry pass.
	spans := startedSpans(sr)
	require.Len(t, spans, 1)
	assert.Equal(t, first, headerValue(t, msg.Headers, "traceparent"),
		"the retry dispatch must not overwrite the injected trace context")
}

func TestConsumerInterceptor_OnConsume(t *testing.T) {
	sr := setupTest(t)

	// Produce a message through the producer interceptor, then consume it
	// with the consumer interceptor: the consumer span must be a child of the
	// producer span, proving the W3C propagation round-trip.
	produced := &sarama.ProducerMessage{
		Topic: "orders",
		Key:   sarama.StringEncoder("order-1"),
		Value: sarama.StringEncoder("hello"),
	}
	(&producerInterceptor{endpoint: "broker:9092", headersSupported: true}).OnSend(produced)

	producerSpan := startedSpans(sr)[0]

	consumed := &sarama.ConsumerMessage{
		Topic:     "orders",
		Partition: 2,
		Offset:    42,
		Key:       []byte("order-1"),
		Value:     []byte("hello"),
		Headers:   toHeaderList(produced.Headers),
	}
	(&consumerInterceptor{endpoint: "broker:9092"}).OnConsume(consumed)

	spans := startedSpans(sr)
	require.Len(t, spans, 2)
	consumerSpan := spans[1]

	assert.Equal(t, "orders receive", consumerSpan.Name())
	assert.Equal(t, trace.SpanKindConsumer, consumerSpan.SpanKind())
	assert.Equal(t, producerSpan.SpanContext().TraceID(), consumerSpan.Parent().TraceID(),
		"consumer span must continue the producer's trace")
	assert.Equal(t, producerSpan.SpanContext().SpanID(), consumerSpan.Parent().SpanID())

	attrs := spanAttrs(consumerSpan)
	assert.Equal(t, "kafka", attrs["messaging.system"])
	assert.Equal(t, "receive", attrs["messaging.operation.name"])
	assert.Equal(t, "orders", attrs["messaging.destination.name"])
	assert.Equal(t, "2", attrs["messaging.destination.partition.id"])
	assert.Equal(t, int64(42), attrs["messaging.kafka.offset"])
	assert.Equal(t, "order-1", attrs["messaging.kafka.message.key"])
	assert.Equal(t, int64(5), attrs["messaging.message.body.size"])
}

func TestConsumerInterceptor_OnConsume_NoTraceHeaders(t *testing.T) {
	sr := setupTest(t)

	msg := &sarama.ConsumerMessage{
		Topic:     "orders",
		Partition: 0,
		Offset:    1,
		Value:     []byte("hello"),
	}
	(&consumerInterceptor{endpoint: "broker:9092"}).OnConsume(msg)

	spans := startedSpans(sr)
	require.Len(t, spans, 1)
	assert.False(t, spans[0].Parent().IsValid(),
		"a message without trace headers must produce a root span")
	assert.Equal(t, "orders receive", spans[0].Name())
}

func TestConsumerInterceptor_OnConsume_NilMessage(t *testing.T) {
	sr := setupTest(t)

	(&consumerInterceptor{endpoint: "broker:9092"}).OnConsume(nil)

	assert.Empty(t, startedSpans(sr))
}

// toHeaderList converts producer record headers ([]RecordHeader) into the
// consumer shape ([]*RecordHeader).
func toHeaderList(headers []sarama.RecordHeader) []*sarama.RecordHeader {
	out := make([]*sarama.RecordHeader, 0, len(headers))
	for i := range headers {
		h := headers[i]
		out = append(out, &h)
	}
	return out
}

// headerValue returns the value of the first header with the given key, or "".
func headerValue(t *testing.T, headers []sarama.RecordHeader, key string) string {
	t.Helper()
	for _, h := range headers {
		if string(h.Key) == key {
			return string(h.Value)
		}
	}
	return ""
}
