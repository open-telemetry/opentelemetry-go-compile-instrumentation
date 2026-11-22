// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package nats provides compile-time OpenTelemetry instrumentation for
// github.com/nats-io/nats.go.
package nats

import (
	"context"
	"sync"
	"time"

	nats "github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	prop "go.opentelemetry.io/otelc/instrumentation/github.com/nats-io/nats.go/internal/propagation"
	"go.opentelemetry.io/otelc/instrumentation/github.com/nats-io/nats.go/semconv"
	"go.opentelemetry.io/otelc/pkg/hook"
	"go.opentelemetry.io/otelc/pkg/runtime"
)

const (
	instrumentationName = "go.opentelemetry.io/otelc/instrumentation/github.com/nats-io/nats.go"
	instrumentationKey  = "NATS"

	publishMsgParamIndex        = 1
	requestMsgParamIndex        = 1
	requestMsgWithCtxParamIndex = 2
	subscribeCbParamIndex       = 2
	queueSubscribeCbParamIndex  = 3
)

func isEnabled() bool {
	return runtime.Instrumented(instrumentationKey)
}

var (
	tracer     trace.Tracer                  //nolint:gochecknoglobals // set once by initInstrumentation
	propagator propagation.TextMapPropagator //nolint:gochecknoglobals // set once by initInstrumentation
	initOnce   sync.Once                     //nolint:gochecknoglobals // guards tracer initialization
)

func initInstrumentation() {
	initOnce.Do(func() {
		tracer = otel.GetTracerProvider().Tracer(
			instrumentationName,
			trace.WithInstrumentationVersion(runtime.ModuleVersion()),
		)
		propagator = otel.GetTextMapPropagator()
		runtime.Logger().Info("NATS (github.com/nats-io/nats.go) instrumentation initialized")
	})
}

func activeContext() context.Context {
	ctx := context.Background()
	if gls := runtime.GetSpanFromGLS(); gls != nil && gls.SpanContext().IsValid() {
		return trace.ContextWithSpan(ctx, gls)
	}
	return ctx
}

func contextWithGLS(ctx context.Context) context.Context {
	if ctx == nil {
		return activeContext()
	}
	if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
		return ctx
	}
	if gls := runtime.GetSpanFromGLS(); gls != nil && gls.SpanContext().IsValid() {
		return trace.ContextWithSpan(ctx, gls)
	}
	return ctx
}

//nolint:spancheck // span is ended in the matching after hook
func startSpan(ctx context.Context, req semconv.Request, kind trace.SpanKind) (context.Context, trace.Span) {
	return tracer.Start(ctx, semconv.SpanName(req),
		trace.WithSpanKind(kind),
		trace.WithAttributes(semconv.TraceAttrs(req)...),
	)
}

func endSpan(ictx hook.HookContext, err error) {
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

func serverAddr(nc *nats.Conn) string {
	if nc != nil {
		return nc.ConnectedAddr()
	}
	return ""
}

// -----------------------------------------------------------------------------
// Producer: (*Conn).Publish(subj, data)
// -----------------------------------------------------------------------------

// BeforePublish starts a send span for (*Conn).Publish.
func BeforePublish(ictx hook.HookContext, nc *nats.Conn, subj string, data []byte) {
	if !runtime.Ready() || !isEnabled() {
		return
	}
	initInstrumentation()

	req := semconv.Request{
		Subject:         subj,
		ServerAddr:      serverAddr(nc),
		Operation:       semconv.OperationSend,
		MessageBodySize: len(data),
	}
	_, span := startSpan(activeContext(), req, trace.SpanKindProducer)
	ictx.SetData(span)
}

// AfterPublish ends the send span.
func AfterPublish(ictx hook.HookContext, err error) {
	endSpan(ictx, err)
}

// -----------------------------------------------------------------------------
// Producer: (*Conn).PublishMsg(msg)
// -----------------------------------------------------------------------------

// BeforePublishMsg starts a send span and injects trace headers into msg.Header.
func BeforePublishMsg(ictx hook.HookContext, nc *nats.Conn, msg *nats.Msg) {
	if !runtime.Ready() || !isEnabled() || msg == nil {
		return
	}
	initInstrumentation()

	req := semconv.Request{
		Subject:         msg.Subject,
		ServerAddr:      serverAddr(nc),
		Operation:       semconv.OperationSend,
		MessageBodySize: len(msg.Data),
	}
	ctx, span := startSpan(activeContext(), req, trace.SpanKindProducer)

	if msg.Header == nil {
		msg.Header = make(nats.Header)
	}
	propagator.Inject(ctx, prop.NewHeaderCarrier(msg.Header))
	ictx.SetParam(publishMsgParamIndex, msg)
	ictx.SetData(span)
}

// AfterPublishMsg ends the send span.
func AfterPublishMsg(ictx hook.HookContext, err error) {
	endSpan(ictx, err)
}

// -----------------------------------------------------------------------------
// Request-Reply: (*Conn).Request(subj, data, timeout)
// -----------------------------------------------------------------------------

// BeforeRequest starts a send span for (*Conn).Request.
func BeforeRequest(ictx hook.HookContext, nc *nats.Conn, subj string, data []byte, _ time.Duration) {
	if !runtime.Ready() || !isEnabled() {
		return
	}
	initInstrumentation()

	req := semconv.Request{
		Subject:         subj,
		ServerAddr:      serverAddr(nc),
		Operation:       semconv.OperationSend,
		MessageBodySize: len(data),
	}
	_, span := startSpan(activeContext(), req, trace.SpanKindProducer)
	ictx.SetData(span)
}

// AfterRequest ends the send span.
func AfterRequest(ictx hook.HookContext, _ *nats.Msg, err error) {
	endSpan(ictx, err)
}

// -----------------------------------------------------------------------------
// Request-Reply: (*Conn).RequestWithContext(ctx, subj, data)
// -----------------------------------------------------------------------------

// BeforeRequestWithContext starts a send span for (*Conn).RequestWithContext.
//
//nolint:revive // ictx and receiver must precede target function arguments in hook signature
func BeforeRequestWithContext(ictx hook.HookContext, nc *nats.Conn, ctx context.Context, subj string, data []byte) {
	if !runtime.Ready() || !isEnabled() {
		return
	}
	initInstrumentation()

	req := semconv.Request{
		Subject:         subj,
		ServerAddr:      serverAddr(nc),
		Operation:       semconv.OperationSend,
		MessageBodySize: len(data),
	}
	_, span := startSpan(contextWithGLS(ctx), req, trace.SpanKindProducer)
	ictx.SetData(span)
}

// AfterRequestWithContext ends the send span.
func AfterRequestWithContext(ictx hook.HookContext, _ *nats.Msg, err error) {
	endSpan(ictx, err)
}

// -----------------------------------------------------------------------------
// Request-Reply: (*Conn).RequestMsg(msg, timeout)
// -----------------------------------------------------------------------------

// BeforeRequestMsg starts a send span and injects trace headers into msg.Header.
func BeforeRequestMsg(ictx hook.HookContext, nc *nats.Conn, msg *nats.Msg, _ time.Duration) {
	if !runtime.Ready() || !isEnabled() || msg == nil {
		return
	}
	initInstrumentation()

	req := semconv.Request{
		Subject:         msg.Subject,
		ServerAddr:      serverAddr(nc),
		Operation:       semconv.OperationSend,
		MessageBodySize: len(msg.Data),
	}
	ctx, span := startSpan(activeContext(), req, trace.SpanKindProducer)

	if msg.Header == nil {
		msg.Header = make(nats.Header)
	}
	propagator.Inject(ctx, prop.NewHeaderCarrier(msg.Header))
	ictx.SetParam(requestMsgParamIndex, msg)
	ictx.SetData(span)
}

// AfterRequestMsg ends the send span.
func AfterRequestMsg(ictx hook.HookContext, _ *nats.Msg, err error) {
	endSpan(ictx, err)
}

// -----------------------------------------------------------------------------
// Request-Reply: (*Conn).RequestMsgWithContext(ctx, msg)
// -----------------------------------------------------------------------------

// BeforeRequestMsgWithContext starts a send span and injects trace headers into msg.Header.
//
//nolint:revive // ictx and receiver must precede target function arguments in hook signature
func BeforeRequestMsgWithContext(ictx hook.HookContext, nc *nats.Conn, ctx context.Context, msg *nats.Msg) {
	if !runtime.Ready() || !isEnabled() || msg == nil {
		return
	}
	initInstrumentation()

	req := semconv.Request{
		Subject:         msg.Subject,
		ServerAddr:      serverAddr(nc),
		Operation:       semconv.OperationSend,
		MessageBodySize: len(msg.Data),
	}
	spanCtx, span := startSpan(contextWithGLS(ctx), req, trace.SpanKindProducer)

	if msg.Header == nil {
		msg.Header = make(nats.Header)
	}
	propagator.Inject(spanCtx, prop.NewHeaderCarrier(msg.Header))
	ictx.SetParam(requestMsgWithCtxParamIndex, msg)
	ictx.SetData(span)
}

// AfterRequestMsgWithContext ends the send span.
func AfterRequestMsgWithContext(ictx hook.HookContext, _ *nats.Msg, err error) {
	endSpan(ictx, err)
}

// -----------------------------------------------------------------------------
// Consumer: Subscribe & QueueSubscribe handlers
// -----------------------------------------------------------------------------

func wrapHandler(nc *nats.Conn, subj, queue string, orig nats.MsgHandler) nats.MsgHandler {
	if orig == nil {
		return nil
	}
	return func(msg *nats.Msg) {
		if !runtime.Ready() || !isEnabled() {
			orig(msg)
			return
		}
		initInstrumentation()

		ctx := context.Background()
		if msg != nil && msg.Header != nil {
			ctx = propagator.Extract(ctx, prop.NewHeaderCarrier(msg.Header))
		}

		destSubj := subj
		var bodySize int
		if msg != nil {
			if msg.Subject != "" {
				destSubj = msg.Subject
			}
			bodySize = len(msg.Data)
		}

		req := semconv.Request{
			Subject:         destSubj,
			Queue:           queue,
			ServerAddr:      serverAddr(nc),
			Operation:       semconv.OperationProcess,
			MessageBodySize: bodySize,
		}
		_, span := startSpan(ctx, req, trace.SpanKindConsumer)
		defer span.End()

		orig(msg)
	}
}

// BeforeSubscribe wraps the MsgHandler callback with tracing.
func BeforeSubscribe(ictx hook.HookContext, nc *nats.Conn, subj string, cb nats.MsgHandler) {
	if !runtime.Ready() || !isEnabled() || cb == nil {
		return
	}
	ictx.SetParam(subscribeCbParamIndex, wrapHandler(nc, subj, "", cb))
}

// BeforeQueueSubscribe wraps the MsgHandler callback with tracing.
func BeforeQueueSubscribe(ictx hook.HookContext, nc *nats.Conn, subj, queue string, cb nats.MsgHandler) {
	if !runtime.Ready() || !isEnabled() || cb == nil {
		return
	}
	ictx.SetParam(queueSubscribeCbParamIndex, wrapHandler(nc, subj, queue, cb))
}
