// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package sarama provides compile-time OpenTelemetry instrumentation for
// github.com/IBM/sarama.
//
// NewClient is the single construction path shared by NewSyncProducer,
// NewAsyncProducer, NewConsumer and NewConsumerGroup, so hooking it once
// appends the OTel interceptors to the shared *sarama.Config and every
// producer and consumer built from that config is traced automatically.
//
// # Span semantics
//
// The interceptor callbacks used here (KIP-42) do not observe acknowledgments:
// OnSend fires when a message is first handed to the
// producer dispatcher, and OnConsume fires when a fetched message is delivered
// to the Messages channel. Neither callback can observe the broker
// acknowledgment, so:
//
//   - Producer spans are created and ended inside OnSend. They reflect local
//     hand-off to the client library, not confirmed broker delivery; the
//     messaging.kafka.async attribute marks this so the span duration is not
//     read as a broker round-trip. A producer span is a root span unless the
//     application puts a context.Context in ProducerMessage.Metadata, which
//     is then used as its parent.
//   - Consumer spans are created and ended inside OnConsume. They reflect
//     message delivery to the consumer, not the application handler's
//     processing time. The W3C trace context is extracted from the message
//     headers when present, so a producer instrumented with OpenTelemetry
//     (this package or any other) is linked to its consumer.
//
// The trace context is injected into the message headers on the producer side
// (before the message is handed to the client), so the consumer side of the
// same topic, instrumented or not, can continue the trace.
package sarama

import (
	"context"
	"sync"

	"github.com/IBM/sarama"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otelc/instrumentation/github.com/IBM/sarama/semconv"
	"go.opentelemetry.io/otelc/pkg/hook"
	"go.opentelemetry.io/otelc/pkg/runtime"
)

const (
	instrumentationName = "go.opentelemetry.io/otelc/" +
		"instrumentation/github.com/IBM/sarama"
	instrumentationKey = "SARAMA"
)

// saramaEnabler controls whether the instrumentation is active, honoring the
// standard otelc environment-variable configuration (e.g.
// OTEL_GO_ENABLED_INSTRUMENTATIONS / OTEL_GO_DISABLED_INSTRUMENTATIONS).
type saramaEnabler struct{}

func (saramaEnabler) Enable() bool {
	return runtime.Instrumented(instrumentationKey)
}

var enabler = saramaEnabler{}

var (
	logger     = runtime.Logger()
	tracer     trace.Tracer
	propagator propagation.TextMapPropagator
	initOnce   sync.Once
)

func initInstrumentation() {
	initOnce.Do(func() {
		tracer = otel.GetTracerProvider().Tracer(
			instrumentationName,
			trace.WithInstrumentationVersion(runtime.ModuleVersion()),
		)
		propagator = otel.GetTextMapPropagator()
		logger.Info("Kafka (IBM/sarama) instrumentation initialized")
	})
}

// BeforeNewClient appends the OpenTelemetry interceptors to conf, the *Config
// shared by every sarama client constructor (NewSyncProducer, NewAsyncProducer,
// NewConsumer and NewConsumerGroup all build on NewClient). It runs before the
// client is created so the appended interceptors are picked up by every
// producer and consumer dispatcher started from this config.
//
// Appending is idempotent: calling BeforeNewClient again with the same conf
// (e.g. when one config is shared by several clients) does not add a second
// copy of the interceptors.
//
// On failure the original call still fails normally; the only observable side
// effect is that the passed conf carries the interceptors, which is harmless
// for retries and for other clients sharing the same config.
func BeforeNewClient(ictx hook.HookContext, addrs []string, conf *sarama.Config) {
	if !enabler.Enable() {
		logger.Debug("Kafka (IBM/sarama) instrumentation disabled")
		return
	}
	if conf == nil {
		conf = sarama.NewConfig()
		ictx.SetParam(1, conf)
	}
	initInstrumentation()

	if !hasProducerInterceptor(conf) {
		conf.Producer.Interceptors = append(conf.Producer.Interceptors,
			&producerInterceptor{
				endpoint:         brokerEndpoint(addrs),
				headersSupported: conf.Version.IsAtLeast(sarama.V0_11_0_0),
			})
	}
	if !hasConsumerInterceptor(conf) {
		conf.Consumer.Interceptors = append(conf.Consumer.Interceptors,
			&consumerInterceptor{endpoint: brokerEndpoint(addrs)})
	}
}

// brokerEndpoint returns the first configured broker address, if any, in
// host:port form. It is captured once at client construction; broker lists
// are conventionally static for the lifetime of a sarama client.
func brokerEndpoint(brokers []string) string {
	if len(brokers) == 0 {
		return ""
	}
	return brokers[0]
}

func hasProducerInterceptor(conf *sarama.Config) bool {
	for _, in := range conf.Producer.Interceptors {
		if _, ok := in.(*producerInterceptor); ok {
			return true
		}
	}
	return false
}

func hasConsumerInterceptor(conf *sarama.Config) bool {
	for _, in := range conf.Consumer.Interceptors {
		if _, ok := in.(*consumerInterceptor); ok {
			return true
		}
	}
	return false
}

// producerInterceptor implements sarama.ProducerInterceptor. It creates a
// producer span when a message is first handed to the dispatcher and injects
// the trace context into the message headers so consumers can link to it.
//
// The span cannot cover broker acknowledgment through this callback, so it
// is ended inside OnSend and marked with messaging.kafka.async. See the
// package-level documentation for the span semantics.
type producerInterceptor struct {
	endpoint string
	// headersSupported records whether the configured Kafka protocol version
	// can carry record headers (Kafka >= 0.11). When false, injecting the
	// trace context would make msg.Headers non-nil, and the sarama dispatcher
	// then rejects every message with
	// ConfigurationError("Producing headers requires Kafka at least v0.11").
	headersSupported bool
}

func (p *producerInterceptor) OnSend(msg *sarama.ProducerMessage) {
	if msg == nil {
		return
	}
	initInstrumentation()

	carrier := recordHeaderCarrier{headers: &msg.Headers}
	if carrier.Get("traceparent") != "" {
		// sarama re-runs the interceptors for every message pulled from the
		// producer input channel, including retries of the same
		// *ProducerMessage (the interceptor loop sits outside the retries==0
		// bookkeeping). A message that already carries an injected trace
		// context has been seen on a previous dispatch, so starting another
		// span here would create one duplicate producer span per retry and
		// overwrite the propagated context. Skip.
		return
	}

	// The message has no context; an application that wants the producer span
	// under a caller's trace can carry the caller's context.Context in
	// ProducerMessage.Metadata, which sarama passes through untouched.
	parent := context.Background()
	if ctx, ok := msg.Metadata.(context.Context); ok {
		parent = ctx
	}

	req := semconv.KafkaRequest{
		Endpoint:        p.endpoint,
		Destination:     msg.Topic,
		Operation:       semconv.KafkaOperationSend,
		MessageKey:      semconv.KafkaMessageKey(encodeKey(msg.Key)),
		MessageBodySize: encodeSize(msg.Value),
		Async:           true,
	}
	spanCtx, span := tracer.Start(parent, spanName(msg.Topic, semconv.KafkaOperationSend),
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(semconv.KafkaRequestTraceAttrs(req)...),
	)
	// Inject before ending so an instrumented consumer can extract this
	// span's trace context from the message headers. This is safe: the
	// dispatcher owns the message at this point and encodes the headers after
	// the interceptor returns. Injection is skipped entirely when the
	// configured protocol version cannot carry headers.
	if p.headersSupported {
		propagator.Inject(spanCtx, carrier)
	}
	span.End()
}

// encodeKey returns the message key bytes, or nil when the key encoder is nil
// or fails to encode.
func encodeKey(key sarama.Encoder) []byte {
	if key == nil {
		return nil
	}
	b, err := key.Encode()
	if err != nil {
		return nil
	}
	return b
}

// encodeSize returns the encoded size of the value encoder without encoding,
// or 0 when the encoder is nil.
func encodeSize(value sarama.Encoder) int {
	if value == nil {
		return 0
	}
	return value.Length()
}

// consumerInterceptor implements sarama.ConsumerInterceptor. It creates a
// consumer span when a fetched message is delivered to the Messages channel,
// extracting the W3C trace context from the message headers when present.
//
// The span cannot cover the application handler's processing time (OnConsume
// is the only consumer callback sarama exposes): see the package-level doc for
// the full semantics.
type consumerInterceptor struct {
	endpoint string
}

func (c *consumerInterceptor) OnConsume(msg *sarama.ConsumerMessage) {
	if msg == nil {
		return
	}
	initInstrumentation()

	parent := propagator.Extract(context.Background(), recordHeaderListCarrier{headers: &msg.Headers})

	req := semconv.KafkaRequest{
		Endpoint:        c.endpoint,
		Destination:     msg.Topic,
		Operation:       semconv.KafkaOperationReceive,
		MessageKey:      semconv.KafkaMessageKey(msg.Key),
		MessageBodySize: len(msg.Value),
		Partition:       int(msg.Partition),
		HasPartition:    true,
		Offset:          msg.Offset,
		HasOffset:       true,
	}
	_, span := tracer.Start(parent, spanName(msg.Topic, semconv.KafkaOperationReceive),
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(semconv.KafkaRequestTraceAttrs(req)...),
	)
	span.End()
}

// spanName returns the messaging span name for a topic and operation,
// following the "{destination} {operation}" convention used across this repo's
// Kafka instrumentation. A missing topic degrades to the operation alone.
func spanName(topic string, op semconv.KafkaOperation) string {
	if topic == "" {
		return string(op)
	}
	return topic + " " + string(op)
}

// recordHeaderCarrier adapts sarama record headers to the OpenTelemetry text
// map carrier. It holds a pointer so Set can grow the caller's header slice:
// on the producer side this is how the injected trace context actually gets
// sent with the message.
type recordHeaderCarrier struct {
	headers *[]sarama.RecordHeader
}

func (c recordHeaderCarrier) Get(key string) string {
	if c.headers == nil {
		return ""
	}
	for _, h := range *c.headers {
		if string(h.Key) == key {
			return string(h.Value)
		}
	}
	return ""
}

func (c recordHeaderCarrier) Set(key, value string) {
	if c.headers == nil {
		return
	}
	for i := range *c.headers {
		if string((*c.headers)[i].Key) == key {
			(*c.headers)[i].Value = []byte(value)
			return
		}
	}
	*c.headers = append(*c.headers, sarama.RecordHeader{
		Key:   []byte(key),
		Value: []byte(value),
	})
}

func (c recordHeaderCarrier) Keys() []string {
	if c.headers == nil {
		return nil
	}
	keys := make([]string, 0, len(*c.headers))
	for _, h := range *c.headers {
		keys = append(keys, string(h.Key))
	}
	return keys
}

// recordHeaderListCarrier adapts sarama consumer record headers (a slice of
// pointers) to the OpenTelemetry text map carrier. The consumer side only
// reads headers, so Set leaves the slice untouched.
type recordHeaderListCarrier struct {
	headers *[]*sarama.RecordHeader
}

func (c recordHeaderListCarrier) Get(key string) string {
	if c.headers == nil {
		return ""
	}
	for _, h := range *c.headers {
		if h != nil && string(h.Key) == key {
			return string(h.Value)
		}
	}
	return ""
}

func (c recordHeaderListCarrier) Set(key, value string) {
	// The consumer side never injects; extracting only needs Get and Keys.
}

func (c recordHeaderListCarrier) Keys() []string {
	if c.headers == nil {
		return nil
	}
	keys := make([]string, 0, len(*c.headers))
	for _, h := range *c.headers {
		if h != nil {
			keys = append(keys, string(h.Key))
		}
	}
	return keys
}
