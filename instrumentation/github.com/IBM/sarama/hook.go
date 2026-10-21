// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package sarama provides compile-time OpenTelemetry instrumentation for
// github.com/IBM/sarama.
//
// NewClient is the single construction path shared by NewSyncProducer,
// NewAsyncProducer, NewConsumer and NewConsumerGroup, so hooking it once
// appends the OTel interceptors to the *Config every producer and consumer is
// built from. NewConsumerGroup and NewConsumerGroupFromClient get their own
// hooks so each group can carry its own messaging.consumer.group.name, even
// when several groups share one Config or one Client.
//
// # Span semantics
//
// The interceptor callbacks used here (KIP-42) do not observe acknowledgments:
// OnConsume fires when a fetched message is delivered to the Messages channel,
// so a consumer span is created and ended inside OnConsume and reflects
// message delivery, not the application handler's processing time.
//
// Producer spans are not ended in OnSend. A span is opened on the first
// dispatch of a message and kept open until the broker result is delivered to
// (*asyncProducer).returnSuccesses, returnErrors or returnError, where it is
// ended (with the error recorded on a failure). Those are the terminal paths
// for every partition/broker producer, for both sync and async producers, so
// the span duration covers the real broker round trip: SendMessage/SendMessages
// errors and partial batch failures are observable, and no
// messaging.kafka.async attribute is needed.
//
// The dispatcher re-runs the producer interceptors on every retry level and
// also routes its internal fin control messages through the same interceptor
// loop. Whether a dispatch is a message's first one (msg.retries == 0 &&
// msg.flags == 0) is answered by an add_file compiled into sarama itself, so
// unexported fields can be read without an import edge from this package;
// retries and internal control messages start no span and inject nothing.
//
// A producer span is a root span unless the application puts a
// context.Context in ProducerMessage.Metadata, which is then used as its
// parent. The W3C trace context is injected into the message headers on the
// first dispatch, so the consumer side of the same topic, instrumented or not,
// can continue the trace.
package sarama

import (
	"context"
	"sync"

	"github.com/IBM/sarama"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
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

	// Positional arguments that the hooks replace via SetParam. NewClient's
	// signature is NewClient(addrs, conf) and NewConsumerGroup's is
	// NewConsumerGroup(addrs, groupID, conf).
	configParamNewClient        = 1
	configParamNewConsumerGroup = 2
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
// passed to NewClient (which NewSyncProducer, NewAsyncProducer and NewConsumer
// build on). It runs before the client is created so the appended
// interceptors are picked up by every producer and consumer started from this
// config.
//
// Appending is idempotent: calling BeforeNewClient again with the same conf
// does not add a second copy of the interceptors.
//
// On failure the original call still fails normally; the only observable side
// effect is that the passed conf carries the interceptors, which is harmless
// for retries and for other clients sharing the same config.
func BeforeNewClient(ictx hook.HookContext, addrs []string, conf *sarama.Config) {
	if conf == nil {
		if enabler.Enable() {
			conf = sarama.NewConfig()
			ictx.SetParam(configParamNewClient, conf)
		} else {
			logger.Debug("Kafka (IBM/sarama) instrumentation disabled")
			return
		}
	}
	if !enabler.Enable() {
		logger.Debug("Kafka (IBM/sarama) instrumentation disabled")
		return
	}
	initInstrumentation()
	installInterceptors(conf, brokerEndpoint(addrs))
}

// BeforeNewConsumerGroup runs before NewConsumerGroup. NewConsumerGroup calls
// NewClient(addrs, conf) internally, so passing the caller's conf here would
// let the inner NewClient hook attach one consumerInterceptor to a config the
// caller may share with another group (or with a plain client): the second
// group would then overwrite the first group's name on that shared
// interceptor.
//
// Instead this hook hands NewClient a private copy of conf carrying its own
// consumerInterceptor tagged with groupID. The caller's conf is never
// mutated, so any number of groups can share it without racing. A nil conf is
// created here (rather than relying on the inner hook) for the same reason.
func BeforeNewConsumerGroup(ictx hook.HookContext, addrs []string, groupID string, conf *sarama.Config) {
	if !enabler.Enable() {
		logger.Debug("Kafka (IBM/sarama) instrumentation disabled")
		return
	}
	initInstrumentation()
	if conf == nil {
		conf = sarama.NewConfig()
	}
	groupConf := configForGroup(conf, brokerEndpoint(addrs), groupID)
	// Replacing the third argument also runs the inner NewClient hook on this
	// private copy: configForGroup has already attached every interceptor, so
	// that hook's idempotent append is a no-op.
	ictx.SetParam(configParamNewConsumerGroup, groupConf)
}

// BeforeNewConsumerGroupFromClient handles groups built on an already-existing
// client (no NewClient call). The client may be shared by several groups, and
// sarama reads client.Config() when it builds the group session, so mutating
// the live config in place would again let one group overwrite another. The
// client is wrapped so Config() returns a private, group-tagged copy while
// every other Client method passes through to the real client.
func BeforeNewConsumerGroupFromClient(ictx hook.HookContext, groupID string, client sarama.Client) {
	if !enabler.Enable() || client == nil {
		return
	}
	initInstrumentation()
	endpoint := ""
	if brokers := client.Brokers(); len(brokers) > 0 && brokers[0] != nil {
		endpoint = brokers[0].Addr()
	}
	conf := client.Config()
	if conf == nil {
		conf = sarama.NewConfig()
	}
	ictx.SetParam(configParamNewClient, &groupClient{
		Client: client,
		conf:   configForGroup(conf, endpoint, groupID),
	})
}

// groupClient wraps a sarama.Client for NewConsumerGroupFromClient, serving a
// private, group-tagged *Config while delegating every other method.
type groupClient struct {
	sarama.Client
	conf *sarama.Config
}

func (c *groupClient) Config() *sarama.Config { return c.conf }

// configForGroup returns a private copy of conf whose consumer interceptors
// are isolated for one group: any application interceptors are kept, a
// previously attached OTel consumer interceptor (present when conf was itself
// used to build a client) is dropped, and one consumerInterceptor tagged with
// groupID is appended. The producer interceptor is carried over untouched;
// producers do not depend on the group. Config holds no lock or atomic value,
// so a value copy is safe.
func configForGroup(conf *sarama.Config, endpoint, groupID string) *sarama.Config {
	clone := *conf

	producers := make([]sarama.ProducerInterceptor, 0, len(conf.Producer.Interceptors)+1)
	producers = append(producers, conf.Producer.Interceptors...)
	if !hasProducerInterceptor(producers) {
		producers = append(producers, newProducerInterceptor(endpoint, conf.Version))
	}
	clone.Producer.Interceptors = producers

	consumers := make([]sarama.ConsumerInterceptor, 0, len(conf.Consumer.Interceptors)+1)
	for _, in := range conf.Consumer.Interceptors {
		if _, ok := in.(*consumerInterceptor); ok {
			continue
		}
		consumers = append(consumers, in)
	}
	consumers = append(consumers, &consumerInterceptor{endpoint: endpoint, group: groupID})
	clone.Consumer.Interceptors = consumers
	return &clone
}

// installInterceptors appends the OpenTelemetry interceptors to conf exactly
// once. It is used for the plain NewClient path; groups get isolated
// interceptors through configForGroup instead.
func installInterceptors(conf *sarama.Config, endpoint string) {
	if conf == nil {
		return
	}
	if !hasProducerInterceptor(conf.Producer.Interceptors) {
		conf.Producer.Interceptors = append(conf.Producer.Interceptors,
			newProducerInterceptor(endpoint, conf.Version))
	}
	if findConsumerInterceptor(conf) == nil {
		conf.Consumer.Interceptors = append(conf.Consumer.Interceptors,
			&consumerInterceptor{endpoint: endpoint})
	}
}

func hasProducerInterceptor(ins []sarama.ProducerInterceptor) bool {
	for _, in := range ins {
		if _, ok := in.(*producerInterceptor); ok {
			return true
		}
	}
	return false
}

func newProducerInterceptor(endpoint string, version sarama.KafkaVersion) *producerInterceptor {
	return &producerInterceptor{
		endpoint:         endpoint,
		headersSupported: version.IsAtLeast(sarama.V0_11_0_0),
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

func findConsumerInterceptor(conf *sarama.Config) *consumerInterceptor {
	for _, in := range conf.Consumer.Interceptors {
		if ci, ok := in.(*consumerInterceptor); ok {
			return ci
		}
	}
	return nil
}

// producerInterceptor implements sarama.ProducerInterceptor. It opens a
// producer span when a message is first dispatched and injects the trace
// context into the message headers so consumers can link to it. The span is
// ended by the returnSuccesses/returnError(s) hooks, not here.
type producerInterceptor struct {
	endpoint string
	// headersSupported records whether the configured Kafka protocol version
	// can carry record headers (Kafka >= 0.11). When false, injecting the
	// trace context would make msg.Headers non-nil, and the sarama dispatcher
	// then rejects every message with
	// ConfigurationError("Producing headers requires Kafka at least v0.11").
	headersSupported bool
}

// firstDispatchFunc reports whether msg is leaving the dispatcher for the
// first time (msg.retries == 0 && msg.flags == 0). It is registered by an
// add_file compiled into sarama (first_dispatch.go), which can read those
// unexported fields without an import edge back to this package. nil means
// the injected file is absent, e.g. when the uninstrumented package is built
// in this repo's unit tests.
var firstDispatchFunc func(*sarama.ProducerMessage) bool

// registerFirstDispatchFunc is the linkname target used by the injected
// sarama-side add_file.
func registerFirstDispatchFunc(f func(*sarama.ProducerMessage) bool) {
	firstDispatchFunc = f
}

// producerSpanState holds the open producer span for one message. It is kept
// in a process-level map keyed by the message pointer (not in
// ProducerMessage.Metadata) so an application that passes a context.Context or
// any other value through Metadata sees that value unchanged on the
// Successes/Errors channel.
type producerSpanState struct {
	span trace.Span
	ctx  context.Context
}

var producerSpans sync.Map // map[*sarama.ProducerMessage]*producerSpanState

func spanStateFor(msg *sarama.ProducerMessage) (*producerSpanState, bool) {
	if msg == nil {
		return nil, false
	}
	s, ok := producerSpans.Load(msg)
	if !ok {
		return nil, false
	}
	state, ok := s.(*producerSpanState)
	return state, ok
}

func storeSpanState(msg *sarama.ProducerMessage, state *producerSpanState) {
	producerSpans.Store(msg, state)
}

// parentContext extracts the caller context an application may carry in
// ProducerMessage.Metadata; sarama passes that field through untouched.
func parentContext(msg *sarama.ProducerMessage) context.Context {
	if ctx, ok := msg.Metadata.(context.Context); ok {
		return ctx
	}
	return context.Background()
}

func (p *producerInterceptor) OnSend(msg *sarama.ProducerMessage) {
	if msg == nil {
		return
	}
	initInstrumentation()

	// sarama re-runs the producer interceptors for every retry level of the
	// same *ProducerMessage, and its internal fin control messages pass
	// through the same interceptor loop. Only the first dispatch
	// (msg.retries == 0 && msg.flags == 0, as reported by the sarama-side
	// add_file) may start a span and inject headers.
	if firstDispatchFunc != nil {
		if !firstDispatchFunc(msg) {
			return
		}
	} else if state, ok := spanStateFor(msg); ok && state != nil {
		// Fallback for builds without the injected add_file (uninstrumented
		// unit-test builds): pointer-keyed dedupe still suppresses retries.
		return
	}

	parent := parentContext(msg)

	req := semconv.KafkaRequest{
		Endpoint:        p.endpoint,
		Destination:     msg.Topic,
		Operation:       semconv.KafkaOperationSend,
		MessageKey:      semconv.KafkaMessageKey(encodeKey(msg.Key)),
		MessageBodySize: encodeSize(msg.Value),
	}
	spanCtx, span := tracer.Start(parent, spanName(msg.Topic, semconv.KafkaOperationSend),
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(semconv.KafkaRequestTraceAttrs(req)...),
	)

	// The span stays open until the broker result arrives at
	// returnSuccesses/returnErrors; it is finished there, not here. Keeping it
	// open is what makes SendMessage/SendMessages errors and partial failures
	// observable and gives sync sends a real broker round-trip duration.
	state := &producerSpanState{span: span, ctx: spanCtx}
	storeSpanState(msg, state)

	// Inject at first dispatch only. Skip entirely when the configured
	// protocol version cannot carry record headers, since the dispatcher
	// rejects non-nil headers on Kafka < 0.11.
	if p.headersSupported {
		propagator.Inject(spanCtx, recordHeaderCarrier{headers: &msg.Headers})
	}
}

// endProducerSpan ends and removes the open span for msg. LoadAndDelete makes
// completion idempotent: sarama versions disagree about the call direction
// between returnError and returnErrors (one calls the other in a loop,
// reversed across releases), so whichever terminal path runs first ends the
// span and the second run is a no-op.
func endProducerSpan(msg *sarama.ProducerMessage, err error) {
	if msg == nil {
		return
	}
	s, loaded := producerSpans.LoadAndDelete(msg)
	if !loaded {
		return
	}
	state, ok := s.(*producerSpanState)
	if !ok || state == nil || state.span == nil {
		return
	}
	if err != nil {
		state.span.RecordError(err)
		state.span.SetStatus(codes.Error, err.Error())
		state.span.SetAttributes(semconv.MessagingErrorType(err))
	}
	state.span.End()
}

// BeforeReturnSuccesses finishes the producer spans after the broker
// acknowledges every message in the batch. returnSuccesses runs for both sync
// and async producers that request Return.Successes.
func BeforeReturnSuccesses(_ hook.HookContext, _ any, batch []*sarama.ProducerMessage) {
	for _, msg := range batch {
		endProducerSpan(msg, nil)
	}
}

// BeforeReturnErrors finishes producer spans for a failed batch. A single
// error (including a sarama ProducerError/ProducerErrors) applies to the whole
// batch, which mirrors how sarama fans the same err back to every message.
func BeforeReturnErrors(_ hook.HookContext, _ any, batch []*sarama.ProducerMessage, err error) {
	for _, msg := range batch {
		endProducerSpan(msg, err)
	}
}

// BeforeReturnError finishes the span for a single failed message.
// returnError is called directly from many failure sites (transaction not
// ready, headers on Kafka < 0.11, MaxMessageBytes exceeded, partition/broker
// producer errors, exhausted retries) and also in one direction of the
// returnError/returnErrors recursion; endProducerSpan's idempotent deletion
// guarantees the span ends exactly once.
func BeforeReturnError(_ hook.HookContext, _ any, msg *sarama.ProducerMessage, err error) {
	endProducerSpan(msg, err)
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
	// group is the consumer group name for group consumers; empty for a plain
	// PartitionConsumer (NewConsumer), which has no group.
	group string
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
		ConsumerGroup:   c.group,
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
