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
	"go.opentelemetry.io/otel/codes"
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
		firstDispatchFunc = nil
		producerSpans = sync.Map{}
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
	registerFirstDispatchFunc(func(*sarama.ProducerMessage) bool { return true })

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
	_, hasAsync := attrs["messaging.kafka.async"]
	assert.False(t, hasAsync, "spans end on the broker result; async must never be set")
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
	registerFirstDispatchFunc(func(*sarama.ProducerMessage) bool { return true })

	(&producerInterceptor{endpoint: "broker:9092"}).OnSend(nil)

	assert.Empty(t, startedSpans(sr))
}

func TestProducerInterceptor_OnSend_MetadataContext(t *testing.T) {
	sr := setupTest(t)
	registerFirstDispatchFunc(func(*sarama.ProducerMessage) bool { return true })

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
	registerFirstDispatchFunc(func(*sarama.ProducerMessage) bool { return true })

	msg := &sarama.ProducerMessage{Value: sarama.StringEncoder("hello")}
	(&producerInterceptor{endpoint: "broker:9092", headersSupported: true}).OnSend(msg)

	spans := startedSpans(sr)
	require.Len(t, spans, 1)
	assert.Equal(t, "send", spans[0].Name())
}

func TestProducerInterceptor_OnSend_OldProtocolSkipsInjection(t *testing.T) {
	sr := setupTest(t)
	registerFirstDispatchFunc(func(*sarama.ProducerMessage) bool { return true })

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

// TestProducerInterceptor_OnlyFirstDispatchStartsSpan is the regression for
// the traceparent heuristic: retries (retries>0) and the dispatcher's
// internal fin messages (flags!=0) pass through the same interceptor loop but
// must neither start a span nor touch the injected headers. The real
// distinction is made by the sarama-side add_file; here its predicate is
// modeled with a map keyed by dispatch sequence.
func TestProducerInterceptor_OnlyFirstDispatchStartsSpan(t *testing.T) {
	sr := setupTest(t)

	type dispatch struct {
		retries int
		flags   int
	}
	registerFirstDispatchFunc(func(msg *sarama.ProducerMessage) bool {
		// The predicate reads retries/flags in the injected file; this fake
		// reproduces that via the matching dispatch descriptor.
		d := msg.Metadata.(*dispatch)
		return d.retries == 0 && d.flags == 0
	})

	p := &producerInterceptor{endpoint: "broker:9092", headersSupported: true}
	msg := &sarama.ProducerMessage{
		Topic:    "orders",
		Value:    sarama.StringEncoder("hello"),
		Metadata: &dispatch{retries: 0, flags: 0},
	}

	p.OnSend(msg)
	tp := headerValue(t, msg.Headers, "traceparent")
	require.NotEmpty(t, tp)
	require.Len(t, startedSpans(sr), 1)

	// Retry dispatch: same *ProducerMessage, retries>0.
	msg.Metadata = &dispatch{retries: 1, flags: 0}
	p.OnSend(msg)
	require.Len(t, startedSpans(sr), 1, "a retry must not start a second span")
	assert.Equal(t, tp, headerValue(t, msg.Headers, "traceparent"),
		"the retry dispatch must not overwrite the injected trace context")

	// Internal fin control message: flags != 0, never a user message.
	fin := &sarama.ProducerMessage{
		Topic:    "orders",
		Metadata: &dispatch{retries: 0, flags: 1},
	}
	p.OnSend(fin)
	require.Len(t, startedSpans(sr), 1, "a fin control message must not start a span")
	assert.Empty(t, fin.Headers, "a fin control message must not be injected")
	if _, ok := spanStateFor(fin); ok {
		t.Fatal("a fin control message must not register span state")
	}
}

func TestConsumerInterceptor_OnConsume(t *testing.T) {
	sr := setupTest(t)
	registerFirstDispatchFunc(func(*sarama.ProducerMessage) bool { return true })

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

func TestProducerInterceptor_SpanStaysOpenUntilSuccess(t *testing.T) {
	sr := setupTest(t)
	registerFirstDispatchFunc(func(*sarama.ProducerMessage) bool { return true })

	p := &producerInterceptor{endpoint: "broker:9092", headersSupported: true}
	msg := &sarama.ProducerMessage{Topic: "orders", Value: sarama.StringEncoder("hello")}
	p.OnSend(msg)

	// Before the broker result, the span is started but not ended.
	started := sr.Started()
	require.Len(t, started, 1)
	assert.Empty(t, sr.Ended(), "span must stay open until the send result arrives")

	BeforeReturnSuccesses(hooktest.NewMockHookContext(), nil, []*sarama.ProducerMessage{msg})

	ended := sr.Ended()
	require.Len(t, ended, 1)
	assert.Equal(t, "orders send", ended[0].Name())
	_, hasAsync := spanAttrs(ended[0])["messaging.kafka.async"]
	assert.False(t, hasAsync)
	if _, ok := spanStateFor(msg); ok {
		t.Fatal("span state must be removed after completion")
	}
}

// TestProducerSpan_OmitsAsyncAttribute covers both Return.Successes settings:
// async producers (Return.Successes=false) get no messaging.kafka.async either,
// because every span ends on the broker result.
func TestProducerSpan_OmitsAsyncAttribute(t *testing.T) {
	for _, returnSuccesses := range []bool{false, true} {
		t.Run("", func(t *testing.T) {
			sr := setupTest(t)
			registerFirstDispatchFunc(func(*sarama.ProducerMessage) bool { return true })

			conf := sarama.NewConfig()
			conf.Producer.Return.Successes = returnSuccesses
			BeforeNewClient(hooktest.NewMockHookContext(conf), []string{"broker:9092"}, conf)

			p := conf.Producer.Interceptors[0].(*producerInterceptor)
			msg := &sarama.ProducerMessage{Topic: "orders", Value: sarama.StringEncoder("hello")}
			p.OnSend(msg)
			BeforeReturnSuccesses(hooktest.NewMockHookContext(), nil, []*sarama.ProducerMessage{msg})

			ended := sr.Ended()
			require.Len(t, ended, 1)
			_, hasAsync := spanAttrs(ended[0])["messaging.kafka.async"]
			assert.False(t, hasAsync)
		})
	}
}

// TestProducerSpan_EndsOnceOnReturnError covers the three completion orderings
// sarama releases exhibit between returnError and returnErrors: only
// returnError, returnError then returnErrors, and returnErrors then
// returnError. Whichever runs first ends the span; the other call is a no-op.
func TestProducerSpan_EndsOnceOnReturnError(t *testing.T) {
	err := sarama.ErrMessageSizeTooLarge

	tests := []struct {
		name string
		end  func(msg *sarama.ProducerMessage)
	}{
		{
			name: "returnError only",
			end: func(msg *sarama.ProducerMessage) {
				BeforeReturnError(hooktest.NewMockHookContext(), nil, msg, err)
			},
		},
		{
			name: "returnError then returnErrors",
			end: func(msg *sarama.ProducerMessage) {
				BeforeReturnError(hooktest.NewMockHookContext(), nil, msg, err)
				BeforeReturnErrors(hooktest.NewMockHookContext(), nil, []*sarama.ProducerMessage{msg}, err)
			},
		},
		{
			name: "returnErrors then returnError",
			end: func(msg *sarama.ProducerMessage) {
				BeforeReturnErrors(hooktest.NewMockHookContext(), nil, []*sarama.ProducerMessage{msg}, err)
				BeforeReturnError(hooktest.NewMockHookContext(), nil, msg, err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := setupTest(t)
			registerFirstDispatchFunc(func(*sarama.ProducerMessage) bool { return true })

			p := &producerInterceptor{endpoint: "broker:9092", headersSupported: true}
			msg := &sarama.ProducerMessage{Topic: "orders", Value: sarama.StringEncoder("hello")}
			p.OnSend(msg)
			tt.end(msg)

			ended := sr.Ended()
			require.Len(t, ended, 1, "the span must end exactly once")
			assert.Equal(t, codes.Error, ended[0].Status().Code)
			assert.Equal(t, err.Error(), ended[0].Status().Description)
			assert.Equal(t, "KError", spanAttrs(ended[0])["error.type"])
			require.Len(t, ended[0].Events(), 1, "the send error must be recorded as an exception event")
			if _, ok := spanStateFor(msg); ok {
				t.Fatal("span state must be removed after completion")
			}
		})
	}
}

func TestProducerInterceptor_SendErrorRecorded(t *testing.T) {
	sr := setupTest(t)
	registerFirstDispatchFunc(func(*sarama.ProducerMessage) bool { return true })

	p := &producerInterceptor{endpoint: "broker:9092", headersSupported: true}
	msg := &sarama.ProducerMessage{Topic: "orders", Value: sarama.StringEncoder("hello")}
	p.OnSend(msg)

	BeforeReturnErrors(hooktest.NewMockHookContext(), nil, []*sarama.ProducerMessage{msg}, sarama.ErrBrokerNotAvailable)

	ended := sr.Ended()
	require.Len(t, ended, 1)
	assert.Equal(t, codes.Error, ended[0].Status().Code)
	assert.Equal(t, sarama.ErrBrokerNotAvailable.Error(), ended[0].Status().Description)
	assert.Equal(t, "KError", spanAttrs(ended[0])["error.type"])
	require.Len(t, ended[0].Events(), 1, "the send error must be recorded as an exception event")
}

func TestConsumerInterceptor_GroupName(t *testing.T) {
	sr := setupTest(t)

	consumed := &sarama.ConsumerMessage{
		Topic:     "orders",
		Partition: 0,
		Offset:    1,
		Value:     []byte("hello"),
	}
	(&consumerInterceptor{endpoint: "broker:9092", group: "checkout-svc"}).OnConsume(consumed)

	span := sr.Ended()[0]
	assert.Equal(t, "checkout-svc", spanAttrs(span)["messaging.consumer.group.name"])
}

// newConsumerGroupConfig runs the NewConsumerGroup hook the way otelc injects
// it and returns the private config handed to the inner NewClient, then runs
// the inner NewClient hook on it (as sarama's NewConsumerGroup would).
func newConsumerGroupConfig(t *testing.T, addrs []string, groupID string, conf *sarama.Config) *sarama.Config {
	t.Helper()
	ictx := hooktest.NewMockHookContext(addrs, groupID, conf)
	BeforeNewConsumerGroup(ictx, addrs, groupID, conf)
	got := ictx.GetParam(2)
	groupConf, ok := got.(*sarama.Config)
	require.True(t, ok, "the hook must replace the config argument with a *sarama.Config")
	require.NotNil(t, groupConf)
	BeforeNewClient(hooktest.NewMockHookContext(addrs, groupConf), addrs, groupConf)
	return groupConf
}

func consumeOnce(t *testing.T, conf *sarama.Config, groupNames *[]string) {
	t.Helper()
	msg := &sarama.ConsumerMessage{Topic: "orders", Partition: 0, Offset: 0, Value: []byte("x")}
	for _, in := range conf.Consumer.Interceptors {
		ci, ok := in.(*consumerInterceptor)
		require.True(t, ok)
		ci.OnConsume(msg)
		*groupNames = append(*groupNames, ci.group)
	}
}

func TestBeforeNewConsumerGroup_NilConfigKeepsGroup(t *testing.T) {
	setupTest(t)

	// Copilot (a): NewConsumerGroup(addrs, groupID, nil) must not lose the
	// group id: the hook creates the default config itself.
	groupConf := newConsumerGroupConfig(t, []string{"broker:9092"}, "g1", nil)

	var names []string
	consumeOnce(t, groupConf, &names)
	require.Equal(t, []string{"g1"}, names)
}

// TestBeforeNewConsumerGroup_SharedConfigKeepsEachGroupName is the regression
// for Copilot (c): two groups built from one caller-owned config must each get
// their own consumer interceptor; the second group must not overwrite the
// first, and the caller's config stays usable as a plain (group-less) client.
func TestBeforeNewConsumerGroup_SharedConfigKeepsEachGroupName(t *testing.T) {
	setupTest(t)

	shared := sarama.NewConfig()
	// The config may already have been through NewClient (e.g. reused).
	BeforeNewClient(hooktest.NewMockHookContext(shared), []string{"broker:9092"}, shared)

	g1 := newConsumerGroupConfig(t, []string{"broker:9092"}, "g1", shared)
	g2 := newConsumerGroupConfig(t, []string{"broker:9092"}, "g2", shared)

	var names []string
	consumeOnce(t, g1, &names)
	consumeOnce(t, g2, &names)
	// The original shared config's own interceptor must be group-less and
	// untouched by either group hook.
	consumeOnce(t, shared, &names)
	assert.Equal(t, []string{"g1", "g2", ""}, names)

	// Isolation also means separate interceptor instances and slices.
	assert.NotSame(t,
		g1.Consumer.Interceptors[len(g1.Consumer.Interceptors)-1],
		g2.Consumer.Interceptors[len(g2.Consumer.Interceptors)-1])
}

// stubClient is a minimal sarama.Client for the FromClient path: it only has
// to serve Config() and Brokers(), which the hook reads.
type stubClient struct {
	sarama.Client
	conf *sarama.Config
}

func (c *stubClient) Config() *sarama.Config { return c.conf }
func (c *stubClient) Brokers() []*sarama.Broker {
	return []*sarama.Broker{sarama.NewBroker("broker:9092")}
}

func newConsumerGroupFromClient(t *testing.T, groupID string, client sarama.Client) sarama.Client {
	t.Helper()
	ictx := hooktest.NewMockHookContext(groupID, client)
	BeforeNewConsumerGroupFromClient(ictx, groupID, client)
	got := ictx.GetParam(1)
	wrapped, ok := got.(sarama.Client)
	require.True(t, ok, "the hook must wrap the client in a sarama.Client")
	require.NotNil(t, wrapped)
	return wrapped
}

// TestBeforeNewConsumerGroupFromClient_SharedClientKeepsEachGroupName is the
// FromClient counterpart: two groups on one shared client get isolated
// configs via the returned wrapper, and the shared client's own config stays
// group-less.
func TestBeforeNewConsumerGroupFromClient_SharedClientKeepsEachGroupName(t *testing.T) {
	setupTest(t)

	shared := sarama.NewConfig()
	BeforeNewClient(hooktest.NewMockHookContext(shared), []string{"broker:9092"}, shared)
	client := &stubClient{conf: shared}

	c1 := newConsumerGroupFromClient(t, "g1", client)
	c2 := newConsumerGroupFromClient(t, "g2", client)

	var names []string
	consumeOnce(t, c1.Config(), &names)
	consumeOnce(t, c2.Config(), &names)
	consumeOnce(t, shared, &names)
	assert.Equal(t, []string{"g1", "g2", ""}, names)

	// The endpoint is still resolved through the wrapped client.
	last := c1.Config().Consumer.Interceptors[len(c1.Config().Consumer.Interceptors)-1].(*consumerInterceptor)
	assert.Equal(t, "broker:9092", last.endpoint)
}
