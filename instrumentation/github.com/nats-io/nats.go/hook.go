// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package nats provides compile-time OpenTelemetry instrumentation for
// github.com/nats-io/nats.go. Limits and attribute choices live in README.md
// in this directory.
package nats

import (
	"context"
	"net/textproto"
	"sync"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otelc/instrumentation/github.com/nats-io/nats.go/semconv"
	"go.opentelemetry.io/otelc/pkg/hook"
	"go.opentelemetry.io/otelc/pkg/runtime"
)

const (
	instrumentationName = "go.opentelemetry.io/otelc/instrumentation/github.com/nats-io/nats.go"
	instrumentationKey  = "NATS"

	// Param indices in (*Conn).publish(subj, reply string, validateReply bool,
	// hdr, data []byte) (receiver is 0). A signature change fails the
	// instrumented build.
	publishHdrIndex  = 4
	publishDataIndex = 5

	// cbIndex is the callback in (*Conn).Subscribe(subject string, cb nats.MsgHandler).
	subscribeCbIndex = 2
	// cbIndex is the callback in (*Conn).QueueSubscribe(subject, queue string,
	// cb nats.MsgHandler).
	queueSubscribeCbIndex = 3

	// hdrLine and crlf are the wire-format constants (*Msg).headerBytes writes.
	// They are duplicated here only to append a trace line to an encoded block;
	// a NATS header block is "NATS/1.0\r\n" + "key: value\r\n"*N + "\r\n".
	hdrLine = "NATS/1.0"
	crlf    = "\r\n"
)

type natsEnabler struct{}

func (natsEnabler) Enable() bool {
	return runtime.Instrumented(instrumentationKey)
}

var (
	enabler    = natsEnabler{}
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
		logger.Info("nats-io/nats.go instrumentation initialized")
	})
}

// shouldTraceSubject reports whether the subject is user messaging traffic.
// Subjects beginning with "$" are reserved by the server for control traffic
// ($SYS.*, $JS.API.*) and for JetStream internal buckets ($KV.*, $O.*); they
// are not user messages and produce no spans.
func shouldTraceSubject(subj string) bool {
	return subj != "" && subj[0] != '$'
}

func parentContext() context.Context {
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

// mapCarrier is a minimal propagation.TextMapCarrier over a string map, used
// to capture the traceparent/tracestate values the configured propagator
// wants to write before they are spliced into the encoded NATS header block.
type mapCarrier map[string]string

func (m mapCarrier) Get(key string) string { return m[key] }
func (m mapCarrier) Set(key, value string) { m[key] = value }
func (m mapCarrier) Keys() []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// headerCarrier is a propagation.TextMapCarrier over a nats.Header. Get tries
// the exact key first (nats.go headers are case-sensitive), then the
// canonicalized form, so trace context written by other producers that
// canonicalize header keys (e.g. the community otelnats wrapper) is also
// found.
type headerCarrier nats.Header

func (h headerCarrier) Get(key string) string {
	if v := nats.Header(h).Get(key); v != "" {
		return v
	}
	return nats.Header(h).Get(textproto.CanonicalMIMEHeaderKey(key))
}

func (h headerCarrier) Set(key, value string) {
	nats.Header(h).Set(key, value)
}

func (h headerCarrier) Keys() []string {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	return keys
}

// -----------------------------------------------------------------------------
// Producer: (*Conn).publish (internal funnel for Publish, PublishMsg,
// PublishRequest, Request, and JetStream publishes)
// -----------------------------------------------------------------------------

// BeforePublish starts a send span and appends the W3C trace context to the
// encoded NATS header block. Hooking the internal publish — the one call every
// publish path funnels through — means a plain Publish is upgraded to a header
// publish (HPUB) exactly like the community otelnats wrapper does by calling
// PublishMsg internally, so every publish path carries the trace.
func BeforePublish(
	ictx hook.HookContext,
	nc *nats.Conn,
	subj, _ /*reply*/ string,
	_ /*validateReply*/ bool,
	hdr, data []byte,
) {
	if !enabler.Enable() || nc == nil {
		return
	}
	if !shouldTraceSubject(subj) {
		return
	}
	initInstrumentation()

	req := semconv.Request{
		Destination:     subj,
		Operation:       semconv.NatsOperationSend,
		Endpoint:        nc.ConnectedAddr(),
		MessageBodySize: len(data),
	}
	ctx, span := startSpan(parentContext(), req, trace.SpanKindProducer)

	// Only inject when the connected server advertises headers; otherwise
	// (*Conn).publish would fail with ErrHeadersNotSupported.
	if nc.HeadersSupported() {
		if newHdr := appendTraceparent(ctx, hdr); newHdr != nil {
			ictx.SetParam(publishHdrIndex, newHdr)
		}
	}

	ictx.SetData(span)
}

// AfterPublish ends the send span. A nil error means the client accepted the
// publish, not that the server confirmed delivery.
func AfterPublish(ictx hook.HookContext, err error) {
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

// appendTraceparent appends the trace lines the configured propagator wants to
// write (traceparent, and tracestate when present) to an encoded NATS header
// block. The block is self-delimiting: it ends with the empty line "\r\n",
// which is replaced by the new lines plus a fresh empty line. When hdr is
// empty a fresh block is built; a non-empty block that does not end with the
// delimiter is left untouched rather than corrupted.
func appendTraceparent(ctx context.Context, hdr []byte) []byte {
	carrier := mapCarrier{}
	propagator.Inject(ctx, carrier)
	tp := carrier["traceparent"]
	if tp == "" {
		return nil
	}
	line := "traceparent: " + tp
	if ts := carrier["tracestate"]; ts != "" {
		line += crlf + "tracestate: " + ts
	}
	line += crlf

	if len(hdr) >= len(crlf) && string(hdr[len(hdr)-len(crlf):]) == crlf {
		out := make([]byte, 0, len(hdr)+len(line))
		out = append(out, hdr[:len(hdr)-len(crlf)]...)
		out = append(out, line...)
		return append(out, crlf...)
	}
	if len(hdr) == 0 {
		return []byte(hdrLine + crlf + line + crlf)
	}
	return nil
}

// -----------------------------------------------------------------------------
// Consumer: (*Conn).Subscribe / QueueSubscribe (async callbacks)
// -----------------------------------------------------------------------------

// BeforeSubscribe wraps the callback so each delivered message starts a process
// span that ends when the callback returns.
func BeforeSubscribe(ictx hook.HookContext, nc *nats.Conn, _ /*subject*/ string, cb nats.MsgHandler) {
	wrapSubscribe(ictx, nc, cb, subscribeCbIndex)
}

// BeforeQueueSubscribe wraps the callback for queue-group subscriptions.
func BeforeQueueSubscribe(
	ictx hook.HookContext,
	nc *nats.Conn,
	_ /*subject*/, _ /*queue*/ string,
	cb nats.MsgHandler,
) {
	wrapSubscribe(ictx, nc, cb, queueSubscribeCbIndex)
}

func wrapSubscribe(ictx hook.HookContext, nc *nats.Conn, cb nats.MsgHandler, cbIndex int) {
	if !enabler.Enable() || nc == nil || cb == nil {
		return
	}
	initInstrumentation()
	endpoint := nc.ConnectedAddr()
	// The named type matters: SetParam stores the dynamic type, and callers
	// type-assert the replaced parameter back to nats.MsgHandler.
	var wrapped nats.MsgHandler = func(m *nats.Msg) {
		if !shouldTraceSubject(m.Subject) {
			cb(m)
			return
		}
		parent := propagator.Extract(context.Background(), headerCarrier(m.Header))
		req := semconv.Request{
			Destination:     m.Subject,
			Operation:       semconv.NatsOperationProcess,
			Endpoint:        endpoint,
			MessageBodySize: len(m.Data),
		}
		_, span := startSpan(parent, req, trace.SpanKindConsumer)
		defer span.End()
		cb(m)
	}
	ictx.SetParam(cbIndex, wrapped)
}

// -----------------------------------------------------------------------------
// Consumer: (*Subscription).NextMsg (sync pull)
// -----------------------------------------------------------------------------

// AfterNextMsg records a receive span for a successfully fetched message. The
// span measures the fetch, not the caller's processing, and ends immediately.
func AfterNextMsg(_ hook.HookContext, msg *nats.Msg, err error) {
	if !enabler.Enable() || msg == nil || err != nil {
		return
	}
	if !shouldTraceSubject(msg.Subject) {
		return
	}
	initInstrumentation()

	parent := propagator.Extract(context.Background(), headerCarrier(msg.Header))
	req := semconv.Request{
		Destination:     msg.Subject,
		Operation:       semconv.NatsOperationReceive,
		MessageBodySize: len(msg.Data),
	}
	// server.address is unavailable here: a Subscription does not expose its
	// Conn (or the connected server address). See README.md.
	_, span := startSpan(parent, req, trace.SpanKindConsumer)
	span.End()
}
