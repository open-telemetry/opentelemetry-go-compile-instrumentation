// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package amqp091 provides compile-time OpenTelemetry instrumentation for
// github.com/rabbitmq/amqp091-go. Limits and attribute choices live in
// README.md in this directory.
package amqp091

import (
	"context"
	"errors"
	"sync"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	amqprop "go.opentelemetry.io/otelc/instrumentation/github.com/rabbitmq/amqp091-go/internal/propagation"
	"go.opentelemetry.io/otelc/instrumentation/github.com/rabbitmq/amqp091-go/semconv"
	"go.opentelemetry.io/otelc/pkg/hook"
	"go.opentelemetry.io/otelc/pkg/runtime"
)

const (
	instrumentationName = "go.opentelemetry.io/otelc/instrumentation/github.com/rabbitmq/amqp091-go"
	instrumentationKey  = "AMQP"

	// publishMsgIndex is Publishing in PublishWithDeferredConfirm
	// (receiver is 0). A signature change fails the instrumented build.
	publishMsgIndex = 5
)

type amqpEnabler struct{}

func (amqpEnabler) Enable() bool {
	return runtime.Instrumented(instrumentationKey)
}

var (
	enabler    = amqpEnabler{}
	logger     = runtime.Logger()
	tracer     trace.Tracer
	propagator propagation.TextMapPropagator
	initOnce   sync.Once

	errDeliveryNotInitialized = errors.New("delivery not initialized")

	publishParents sync.Map // *amqp.Channel -> context.Context
	channelAcks    sync.Map // *amqp.Channel -> *pendingAcks
)

func initInstrumentation() {
	initOnce.Do(func() {
		tracer = otel.GetTracerProvider().Tracer(
			instrumentationName,
			trace.WithInstrumentationVersion(runtime.ModuleVersion()),
		)
		propagator = otel.GetTextMapPropagator()
		logger.Info("rabbitmq/amqp091-go instrumentation initialized")
	})
}

func parentContext(ch *amqp.Channel) context.Context {
	if ch != nil {
		if v, ok := publishParents.LoadAndDelete(ch); ok {
			if ctx, ok := v.(context.Context); ok && ctx != nil {
				if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
					return ctx
				}
			}
		}
	}
	ctx := context.Background()
	if span := runtime.GetSpanFromGLS(); span != nil && span.SpanContext().IsValid() {
		return trace.ContextWithSpan(ctx, span)
	}
	return ctx
}

func startSpan(ctx context.Context, req semconv.Request, kind trace.SpanKind) (context.Context, trace.Span) {
	return tracer.Start(ctx, semconv.SpanName(req),
		trace.WithSpanKind(kind),
		trace.WithAttributes(semconv.TraceAttrs(req)...),
	)
}

// cloneTable copies top-level keys only. Inject adds string headers and does
// not mutate nested Table values, so a shallow copy does not alias the
// caller's map.
func cloneTable(t amqp.Table) amqp.Table {
	out := make(amqp.Table, len(t))
	for k, v := range t {
		out[k] = v
	}
	return out
}

func stashPublishParent(ch *amqp.Channel, ctx context.Context) {
	if !enabler.Enable() || ch == nil || ctx == nil {
		return
	}
	publishParents.Store(ch, ctx)
}

func acksFor(ch *amqp.Channel) *pendingAcks {
	actual, _ := channelAcks.LoadOrStore(ch, &pendingAcks{})
	return actual.(*pendingAcks)
}

func acksLookup(ch *amqp.Channel) *pendingAcks {
	if ch == nil {
		return nil
	}
	v, ok := channelAcks.Load(ch)
	if !ok {
		return nil
	}
	return v.(*pendingAcks)
}

// -----------------------------------------------------------------------------
// Producer: context stash for PublishWithContext*
// -----------------------------------------------------------------------------

// BeforePublishWithContext stashes the API context. The library drops it
// before PublishWithDeferredConfirm.
func BeforePublishWithContext(
	_ hook.HookContext,
	ch *amqp.Channel,
	ctx context.Context,
	_ /*exchange*/, _ /*key*/ string,
	_ /*mandatory*/, _ /*immediate*/ bool,
	_ amqp.Publishing,
) {
	stashPublishParent(ch, ctx)
}

// BeforePublishWithDeferredConfirmWithContext stashes the API context for
// the inner PublishWithDeferredConfirm hook.
func BeforePublishWithDeferredConfirmWithContext(
	_ hook.HookContext,
	ch *amqp.Channel,
	ctx context.Context,
	_ /*exchange*/, _ /*key*/ string,
	_ /*mandatory*/, _ /*immediate*/ bool,
	_ amqp.Publishing,
) {
	stashPublishParent(ch, ctx)
}

// -----------------------------------------------------------------------------
// Producer: (*Channel).PublishWithDeferredConfirm
// -----------------------------------------------------------------------------

// BeforePublishWithDeferredConfirm starts a send span and injects W3C
// trace context into a copy of Publishing.Headers.
func BeforePublishWithDeferredConfirm(
	ictx hook.HookContext,
	ch *amqp.Channel,
	exchange, key string,
	_ /*mandatory*/, _ /*immediate*/ bool,
	msg amqp.Publishing,
) {
	if !enabler.Enable() {
		logger.Debug("amqp091 instrumentation disabled")
		return
	}
	initInstrumentation()

	req := semconv.Request{
		Exchange:        exchange,
		RoutingKey:      key,
		Operation:       semconv.OperationSend,
		MessageBodySize: len(msg.Body),
	}
	ctx, span := startSpan(parentContext(ch), req, trace.SpanKindProducer)
	msg.Headers = cloneTable(msg.Headers)
	propagator.Inject(ctx, amqprop.NewTableCarrier(&msg.Headers))
	ictx.SetParam(publishMsgIndex, msg)
	ictx.SetData(span)
}

// AfterPublishWithDeferredConfirm ends the send span. A nil error means the
// client accepted the publish, not that the broker confirmed it.
func AfterPublishWithDeferredConfirm(ictx hook.HookContext, _ *amqp.DeferredConfirmation, err error) {
	span, ok := ictx.GetData().(trace.Span)
	if !ok || span == nil {
		return
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}

// -----------------------------------------------------------------------------
// Consumer: (*Channel).Consume / ConsumeWithContext / Get
// -----------------------------------------------------------------------------

type consumeData struct {
	ch      *amqp.Channel
	queue   string
	autoAck bool
}

func storeConsume(ictx hook.HookContext, ch *amqp.Channel, queue string, autoAck bool) {
	if !enabler.Enable() {
		logger.Debug("amqp091 instrumentation disabled")
		return
	}
	initInstrumentation()
	ictx.SetData(&consumeData{ch: ch, queue: queue, autoAck: autoAck})
}

// BeforeConsume stores the queue and auto-ack flag for AfterConsume.
func BeforeConsume(
	ictx hook.HookContext,
	ch *amqp.Channel,
	queue, _ /*consumer*/ string,
	autoAck, _ /*exclusive*/, _ /*noLocal*/, _ /*noWait*/ bool,
	_ amqp.Table,
) {
	storeConsume(ictx, ch, queue, autoAck)
}

// BeforeConsumeWithContext stores the same consumeData as BeforeConsume.
func BeforeConsumeWithContext(
	ictx hook.HookContext,
	ch *amqp.Channel,
	_ context.Context,
	queue, consumer string,
	autoAck, exclusive, noLocal, noWait bool,
	args amqp.Table,
) {
	BeforeConsume(ictx, ch, queue, consumer, autoAck, exclusive, noLocal, noWait, args)
}

// AfterConsume replaces the delivery channel so each Delivery carries a span.
// Enable is not re-checked. BeforeConsume's only early return is disable,
// which leaves GetData empty. After still skips the wrap when Consume
// returns an error or a nil channel, even if data is set.
func AfterConsume(ictx hook.HookContext, deliveries <-chan amqp.Delivery, err error) {
	data, ok := ictx.GetData().(*consumeData)
	if !ok || data == nil || err != nil || deliveries == nil {
		return
	}
	ictx.SetReturnVal(0, wrapDeliveries(data.ch, data.queue, data.autoAck, deliveries))
}

// BeforeGet stores the queue and auto-ack flag for AfterGet.
func BeforeGet(ictx hook.HookContext, ch *amqp.Channel, queue string, autoAck bool) {
	storeConsume(ictx, ch, queue, autoAck)
}

// AfterGet attaches a consumer span to a successful Get delivery.
// There is no consume channel to close, so an unacked Get process span
// stays open until the process exits. See README.md.
func AfterGet(ictx hook.HookContext, msg amqp.Delivery, ok bool, err error) {
	data, got := ictx.GetData().(*consumeData)
	if !got || data == nil || err != nil || !ok {
		return
	}
	ictx.SetReturnVal(0, startDeliverySpan(data.ch, data.queue, data.autoAck, msg, nil))
}

func wrapDeliveries(ch *amqp.Channel, queue string, autoAck bool, in <-chan amqp.Delivery) <-chan amqp.Delivery {
	// Library Consume channels are unbuffered (amqp091-go channel.go).
	// The wrapper is unbuffered too, so the consumer still blocks the
	// library the same way.
	out := make(chan amqp.Delivery)
	pending := &pendingAcks{}
	go func() {
		defer close(out)
		defer pending.endAll()
		for d := range in {
			out <- startDeliverySpan(ch, queue, autoAck, d, pending)
		}
	}()
	return out
}

func startDeliverySpan(ch *amqp.Channel, queue string, autoAck bool, d amqp.Delivery, local *pendingAcks) amqp.Delivery {
	op := semconv.OperationProcess
	if autoAck {
		op = semconv.OperationReceive
	}
	req := semconv.Request{
		Exchange:        d.Exchange,
		RoutingKey:      d.RoutingKey,
		Queue:           queue,
		Operation:       op,
		MessageBodySize: len(d.Body),
	}
	parent := propagator.Extract(context.Background(), amqprop.NewTableCarrier(&d.Headers))
	_, span := startSpan(parent, req, trace.SpanKindConsumer)
	if autoAck {
		// Auto-ack span ends when the Delivery is read. See README.md.
		span.End()
		return d
	}
	s := &settlingAcknowledger{
		inner:   d.Acknowledger,
		span:    span,
		local:   local,
		channel: nil,
	}
	if ch != nil {
		s.channel = acksFor(ch)
	}
	if local != nil {
		local.add(d.DeliveryTag, s)
	}
	if s.channel != nil {
		s.channel.add(d.DeliveryTag, s)
	}
	d.Acknowledger = s
	return d
}

// -----------------------------------------------------------------------------
// Settlement: Channel.Ack / Nack / Reject
// -----------------------------------------------------------------------------

type ackCall struct {
	ch       *amqp.Channel
	tag      uint64
	multiple bool
}

func storeAck(ictx hook.HookContext, ch *amqp.Channel, tag uint64, multiple bool) {
	if !enabler.Enable() {
		return
	}
	ictx.SetData(&ackCall{ch: ch, tag: tag, multiple: multiple})
}

// BeforeAck records the channel settle call for AfterAck.
func BeforeAck(ictx hook.HookContext, ch *amqp.Channel, tag uint64, multiple bool) {
	storeAck(ictx, ch, tag, multiple)
}

// AfterAck ends process spans for Channel.Ack, including multiple=true.
func AfterAck(ictx hook.HookContext, err error) {
	afterChannelSettle(ictx, err)
}

// BeforeNack records the channel settle call for AfterNack.
func BeforeNack(ictx hook.HookContext, ch *amqp.Channel, tag uint64, multiple, _ /*requeue*/ bool) {
	storeAck(ictx, ch, tag, multiple)
}

// AfterNack ends process spans for Channel.Nack. A successful nack is a
// finish, not a failed operation.
func AfterNack(ictx hook.HookContext, err error) {
	afterChannelSettle(ictx, err)
}

// BeforeReject records the channel settle call for AfterReject.
func BeforeReject(ictx hook.HookContext, ch *amqp.Channel, tag uint64, _ /*requeue*/ bool) {
	storeAck(ictx, ch, tag, false)
}

// AfterReject ends process spans for Channel.Reject. A successful reject is a
// finish, not a failed operation.
func AfterReject(ictx hook.HookContext, err error) {
	afterChannelSettle(ictx, err)
}

// -----------------------------------------------------------------------------
// Channel lifecycle: (*Channel).Close
// -----------------------------------------------------------------------------

// BeforeClose ends any process spans still awaiting acknowledgement on ch and
// drops ch's entries from publishParents and channelAcks. Without this,
// closing a channel leaves its *Channel pointer, and any still-open spans it
// holds, reachable from these package-level maps forever.
func BeforeClose(_ hook.HookContext, ch *amqp.Channel) {
	if ch == nil {
		return
	}
	publishParents.Delete(ch)
	if v, ok := channelAcks.LoadAndDelete(ch); ok {
		if p, ok := v.(*pendingAcks); ok {
			p.endAll()
		}
	}
}

func afterChannelSettle(ictx hook.HookContext, err error) {
	call, ok := ictx.GetData().(*ackCall)
	if !ok || call == nil {
		return
	}
	if p := acksLookup(call.ch); p != nil {
		p.settle(call.tag, call.multiple, err)
	}
}

type pendingAcks struct {
	mu   sync.Mutex
	acks map[uint64]*settlingAcknowledger
}

func (p *pendingAcks) add(tag uint64, s *settlingAcknowledger) {
	p.mu.Lock()
	if p.acks == nil {
		p.acks = map[uint64]*settlingAcknowledger{}
	}
	p.acks[tag] = s
	p.mu.Unlock()
}

func (p *pendingAcks) settle(tag uint64, multiple bool, err error) {
	var toEnd []*settlingAcknowledger
	p.mu.Lock()
	if multiple {
		for t, s := range p.acks {
			if t <= tag {
				toEnd = append(toEnd, s)
				delete(p.acks, t)
			}
		}
	} else if s, ok := p.acks[tag]; ok {
		toEnd = append(toEnd, s)
		delete(p.acks, tag)
	}
	p.mu.Unlock()
	for _, s := range toEnd {
		s.end(err)
	}
}

func (p *pendingAcks) endAll() {
	p.mu.Lock()
	left := p.acks
	p.acks = nil
	p.mu.Unlock()
	for _, s := range left {
		s.end(nil)
	}
}

type settlingAcknowledger struct {
	inner   amqp.Acknowledger
	span    trace.Span
	once    sync.Once
	local   *pendingAcks
	channel *pendingAcks
}

func (a *settlingAcknowledger) Ack(tag uint64, multiple bool) error {
	err := a.callInner(func() error { return a.inner.Ack(tag, multiple) })
	a.finish(tag, multiple, err)
	return err
}

func (a *settlingAcknowledger) Nack(tag uint64, multiple, requeue bool) error {
	err := a.callInner(func() error { return a.inner.Nack(tag, multiple, requeue) })
	a.finish(tag, multiple, err)
	return err
}

func (a *settlingAcknowledger) Reject(tag uint64, requeue bool) error {
	err := a.callInner(func() error { return a.inner.Reject(tag, requeue) })
	a.finish(tag, false, err)
	return err
}

func (a *settlingAcknowledger) callInner(fn func() error) error {
	if a.inner == nil {
		return errDeliveryNotInitialized
	}
	return fn()
}

func (a *settlingAcknowledger) finish(tag uint64, multiple bool, err error) {
	n := 0
	if a.local != nil {
		a.local.settle(tag, multiple, err)
		n++
	}
	if a.channel != nil {
		a.channel.settle(tag, multiple, err)
		n++
	}
	if n == 0 {
		a.end(err)
	}
}

func (a *settlingAcknowledger) end(err error) {
	a.once.Do(func() {
		if a.span == nil {
			return
		}
		if err != nil {
			a.span.RecordError(err)
			a.span.SetStatus(codes.Error, err.Error())
		}
		a.span.End()
	})
}
