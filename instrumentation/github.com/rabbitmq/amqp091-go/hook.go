// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package amqp091 provides compile-time OpenTelemetry instrumentation for
// github.com/rabbitmq/amqp091-go.
//
// Publish, PublishWithContext, and PublishWithDeferredConfirmWithContext all
// call (*Channel).PublishWithDeferredConfirm. That is the only publish hook,
// so one send is one PRODUCER span. The library drops the PublishWithContext
// context before that call, so the span parents from the GLS span (the inbound
// HTTP or gRPC span) when one is present. Publisher confirms do not extend
// the span: AfterPublishWithDeferredConfirm ends it when the write is handed
// to the client, the same limit kafka-go has for async writes.
//
// Consume and ConsumeWithContext return a delivery channel. The after hook
// replaces that channel. Each Delivery starts a CONSUMER span. Manual-ack
// spans use operation process and end on Delivery.Ack, Nack, or Reject.
// Auto-ack spans use operation receive and end as soon as the Delivery is
// taken from the channel. Channel.Ack / Channel.Nack bypass Delivery and
// do not end these spans. Closing the consume channel ends leftover process
// spans so they do not leak.
//
// (*Channel).connection is unexported, so these spans do not set
// server.address. The instrumentation is trace-only.
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

func parentContext() context.Context {
	ctx := context.Background()
	if span := runtime.GetSpanFromGLS(); span != nil && span.SpanContext().IsValid() {
		return trace.ContextWithSpan(ctx, span)
	}
	return ctx
}

// -----------------------------------------------------------------------------
// Producer: (*Channel).PublishWithDeferredConfirm
// -----------------------------------------------------------------------------

// BeforePublishWithDeferredConfirm starts a send span and injects W3C
// trace context into Publishing.Headers.
func BeforePublishWithDeferredConfirm(
	ictx hook.HookContext,
	_ *amqp.Channel,
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
	ctx, span := tracer.Start(parentContext(), semconv.SpanName(req),
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(semconv.TraceAttrs(req)...),
	)
	if msg.Headers == nil {
		msg.Headers = amqp.Table{}
	}
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
// Consumer: (*Channel).Consume / ConsumeWithContext
// -----------------------------------------------------------------------------

type consumeData struct {
	queue   string
	autoAck bool
}

// BeforeConsume stores the queue and auto-ack flag for AfterConsume.
func BeforeConsume(
	ictx hook.HookContext,
	_ *amqp.Channel,
	queue, _ /*consumer*/ string,
	autoAck, _ /*exclusive*/, _ /*noLocal*/, _ /*noWait*/ bool,
	_ amqp.Table,
) {
	if !enabler.Enable() {
		logger.Debug("amqp091 instrumentation disabled")
		return
	}
	initInstrumentation()
	ictx.SetData(&consumeData{queue: queue, autoAck: autoAck})
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
// The Enable() check is omitted: if BeforeConsume was disabled, GetData is
// empty. Re-checking here could skip the wrap if the flag flipped between
// Before and After.
func AfterConsume(ictx hook.HookContext, deliveries <-chan amqp.Delivery, err error) {
	data, ok := ictx.GetData().(*consumeData)
	if !ok || data == nil || err != nil || deliveries == nil {
		return
	}
	ictx.SetReturnVal(0, wrapDeliveries(data.queue, data.autoAck, deliveries))
}

// -----------------------------------------------------------------------------
// Consumer: (*Channel).Get
// -----------------------------------------------------------------------------

// BeforeGet stores the queue and auto-ack flag for AfterGet.
func BeforeGet(ictx hook.HookContext, _ *amqp.Channel, queue string, autoAck bool) {
	if !enabler.Enable() {
		logger.Debug("amqp091 instrumentation disabled")
		return
	}
	initInstrumentation()
	ictx.SetData(&consumeData{queue: queue, autoAck: autoAck})
}

// AfterGet attaches a consumer span to a successful Get delivery.
func AfterGet(ictx hook.HookContext, msg amqp.Delivery, ok bool, err error) {
	data, got := ictx.GetData().(*consumeData)
	if !got || data == nil || err != nil || !ok {
		return
	}
	ictx.SetReturnVal(0, startDeliverySpan(data.queue, data.autoAck, msg))
}

func wrapDeliveries(queue string, autoAck bool, in <-chan amqp.Delivery) <-chan amqp.Delivery {
	out := make(chan amqp.Delivery)
	pending := &pendingSpans{}
	go func() {
		defer close(out)
		defer pending.endAll()
		for d := range in {
			d = startDeliverySpan(queue, autoAck, d)
			if s, ok := d.Acknowledger.(*settlingAcknowledger); ok {
				pending.add(s)
			}
			out <- d
		}
	}()
	return out
}

func startDeliverySpan(queue string, autoAck bool, d amqp.Delivery) amqp.Delivery {
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
	_, span := tracer.Start(parent, semconv.SpanName(req),
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(semconv.TraceAttrs(req)...),
	)
	if autoAck {
		span.End()
		return d
	}
	d.Acknowledger = &settlingAcknowledger{inner: d.Acknowledger, span: span}
	return d
}

type pendingSpans struct {
	mu    sync.Mutex
	spans []*settlingAcknowledger
}

func (p *pendingSpans) add(s *settlingAcknowledger) {
	p.mu.Lock()
	p.spans = append(p.spans, s)
	p.mu.Unlock()
}

func (p *pendingSpans) endAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, s := range p.spans {
		s.end(nil)
	}
	p.spans = nil
}

type settlingAcknowledger struct {
	inner amqp.Acknowledger
	span  trace.Span
	once  sync.Once
}

func (a *settlingAcknowledger) Ack(tag uint64, multiple bool) error {
	if a.inner == nil {
		err := errDeliveryNotInitialized
		a.end(err)
		return err
	}
	err := a.inner.Ack(tag, multiple)
	a.end(err)
	return err
}

func (a *settlingAcknowledger) Nack(tag uint64, multiple, requeue bool) error {
	if a.inner == nil {
		err := errDeliveryNotInitialized
		a.end(err)
		return err
	}
	err := a.inner.Nack(tag, multiple, requeue)
	if err == nil {
		a.endStatus(codes.Error, "nack")
		return nil
	}
	a.end(err)
	return err
}

func (a *settlingAcknowledger) Reject(tag uint64, requeue bool) error {
	if a.inner == nil {
		err := errDeliveryNotInitialized
		a.end(err)
		return err
	}
	err := a.inner.Reject(tag, requeue)
	if err == nil {
		a.endStatus(codes.Error, "reject")
		return nil
	}
	a.end(err)
	return err
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

func (a *settlingAcknowledger) endStatus(code codes.Code, msg string) {
	a.once.Do(func() {
		if a.span == nil {
			return
		}
		a.span.SetStatus(code, msg)
		a.span.End()
	})
}
