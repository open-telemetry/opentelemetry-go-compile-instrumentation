// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package nats

import (
	"context"
	"sync"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/propagators/b3"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otelc/pkg/hook/hooktest"
)

func setupTest(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nats")

	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))

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

func spanAttrs(span sdktrace.ReadOnlySpan) map[string]any {
	m := make(map[string]any)
	for _, a := range span.Attributes() {
		m[string(a.Key)] = a.Value.AsInterface()
	}
	return m
}

func TestNatsEnabler(t *testing.T) {
	tests := []struct {
		name     string
		setupEnv func(t *testing.T)
		want     bool
	}{
		{
			name: "enabled explicitly",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nats")
			},
			want: true,
		},
		{
			name: "disabled explicitly",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "nats")
			},
			want: false,
		},
		{
			name: "not in enabled list",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp")
			},
			want: false,
		},
		{
			name:     "default enabled when no env set",
			setupEnv: func(t *testing.T) {},
			want:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupEnv(t)
			assert.Equal(t, tt.want, enabler.Enable())
		})
	}
}

func TestBeforePublish_SkipsWhenDisabled(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp")

	ictx := hooktest.NewMockHookContext(&nats.Conn{}, "orders.created", "", false, []byte(nil), []byte("hi"))
	BeforePublish(ictx, &nats.Conn{}, "orders.created", "", false, []byte(nil), []byte("hi"))

	// The param must be left untouched: the hdr argument is still the original.
	hdr, ok := ictx.GetParam(publishHdrIndex).([]byte)
	require.True(t, ok)
	assert.Nil(t, hdr)
	_, hasData := ictx.GetData().(trace.Span)
	assert.False(t, hasData, "no span must be stored when the instrumentation is disabled")
}

func TestBeforePublish_StartsSendSpan(t *testing.T) {
	sr := setupTest(t)

	// A zero-value *nats.Conn reports HeadersSupported()==false, so the span is
	// created and no trace context is injected. Injection is exercised by
	// TestAppendTraceparent and the integration test against a real server.
	ictx := hooktest.NewMockHookContext(&nats.Conn{}, "orders.created", "", false, []byte(nil), []byte("hi"))
	BeforePublish(ictx, &nats.Conn{}, "orders.created", "", false, []byte(nil), []byte("hi"))

	spans := sr.Started()
	require.Len(t, spans, 1)
	require.Equal(t, "orders.created send", spans[0].Name())
	require.Equal(t, trace.SpanKindProducer, spans[0].SpanKind())
	attrs := spanAttrs(spans[0])
	require.Equal(t, "nats", attrs["messaging.system"])
	require.Equal(t, "send", attrs["messaging.operation.name"])
	require.Equal(t, "send", attrs["messaging.operation.type"])
	require.Equal(t, "orders.created", attrs["messaging.destination.name"])
	_, hasAddr := attrs["server.address"]
	require.False(t, hasAddr, "zero-value Conn exposes no server address")
	assert.Equal(t, int64(2), attrs["messaging.message.body.size"])

	AfterPublish(ictx, nil)
	assert.Len(t, sr.Ended(), 1)
}

func TestBeforePublish_SkipsControlSubjects(t *testing.T) {
	sr := setupTest(t)

	for _, subj := range []string{"$JS.API.STREAM.CREATE", "$SYS.REQ.SERVER.PING", ""} {
		ictx := hooktest.NewMockHookContext(&nats.Conn{}, subj, "", false, []byte(nil), []byte("hi"))
		BeforePublish(ictx, &nats.Conn{}, subj, "", false, []byte(nil), []byte("hi"))
		assert.Empty(t, sr.Started(), "no span for subject %q", subj)
	}
}

func TestAfterPublish_RecordsError(t *testing.T) {
	sr := setupTest(t)

	ictx := hooktest.NewMockHookContext(&nats.Conn{}, "orders.created", "", false, []byte(nil), []byte("hi"))
	BeforePublish(ictx, &nats.Conn{}, "orders.created", "", false, []byte(nil), []byte("hi"))

	sentinel := nats.ErrConnectionClosed
	AfterPublish(ictx, sentinel)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Error, spans[0].Status().Code)
	require.Equal(t, sentinel.Error(), spans[0].Status().Description)
	require.NotEmpty(t, spans[0].Events(), "the error must be recorded on the span")
}

func TestAppendTraceparent(t *testing.T) {
	setupTest(t) // sets the propagator; initOnce is consumed so it stays put

	tests := []struct {
		name string
		hdr  []byte
	}{
		{name: "existing header block", hdr: []byte("NATS/1.0\r\nFoo: bar\r\n\r\n")},
		{name: "empty header", hdr: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Inject into the context of an active span: the propagator only
			// writes traceparent for a valid trace context, mirroring the real
			// flow where BeforePublish starts the span first.
			ctx, span := tracer.Start(context.Background(), "test")
			defer span.End()

			out := appendTraceparent(ctx, tt.hdr)
			require.NotNil(t, out, "a valid trace context must always inject")

			h, err := nats.DecodeHeadersMsg(out)
			require.NoError(t, err, "the mutated block must remain a valid NATS header block")
			require.NotEmpty(t, h.Get("traceparent"), "the block must carry the W3C trace context")

			// The original headers survive the append.
			if len(tt.hdr) > 0 {
				require.Equal(t, "bar", h.Get("Foo"))
			}
		})
	}
}

func TestAppendTraceparent_MalformedBlockUntouched(t *testing.T) {
	setupTest(t)

	// A header block that does not end with the delimiter is not a valid NATS
	// block; leave the message untouched rather than corrupting it.
	bad := []byte("NATS/1.0\r\nFoo: bar")
	assert.Nil(t, appendTraceparent(context.Background(), bad))
}

// TestAppendTraceparent_B3Propagator proves a B3-configured propagator's headers
// are preserved on the producer path, instead of being dropped like the W3C-only
// extraction did before the fix. Both B3 encodings are enabled so the check
// covers the single `b3` header and the `x-b3-*` multiple headers.
func TestAppendTraceparent_B3Propagator(t *testing.T) {
	setupTest(t)
	propagator = b3.New(b3.WithInjectEncoding(b3.B3MultipleHeader | b3.B3SingleHeader))

	ctx, span := tracer.Start(context.Background(), "test")
	defer span.End()

	out := appendTraceparent(ctx, nil)
	require.NotNil(t, out, "a valid trace context must always inject")

	h, err := nats.DecodeHeadersMsg(out)
	require.NoError(t, err, "the mutated block must remain a valid NATS header block")
	require.NotEmpty(t, h.Get("b3"), "the B3 single header must survive")
	require.NotEmpty(t, h.Get("x-b3-traceid"), "the B3 trace id must survive")
	require.NotEmpty(t, h.Get("x-b3-spanid"), "the B3 span id must survive")
	require.NotEmpty(t, h.Get("x-b3-sampled"), "the B3 sampling state must survive")
	_, hasTraceparent := h["traceparent"]
	assert.False(t, hasTraceparent, "the B3 propagator does not write a W3C traceparent")
}

// TestAppendTraceparent_CompositePropagator checks that a composite propagator
// contributes every header it writes: W3C keys and B3 keys coexist in the block,
// and the pre-existing application header survives the append.
func TestAppendTraceparent_CompositePropagator(t *testing.T) {
	setupTest(t)
	propagator = propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		b3.New(b3.WithInjectEncoding(b3.B3MultipleHeader|b3.B3SingleHeader)),
	)

	ctx, span := tracer.Start(context.Background(), "test")
	defer span.End()

	out := appendTraceparent(ctx, []byte("NATS/1.0\r\nFoo: bar\r\n\r\n"))
	require.NotNil(t, out)

	h, err := nats.DecodeHeadersMsg(out)
	require.NoError(t, err)
	require.NotEmpty(t, h.Get("traceparent"), "the W3C trace context must survive")
	require.NotEmpty(t, h.Get("x-b3-traceid"), "the B3 trace id must survive")
	require.NotEmpty(t, h.Get("x-b3-spanid"), "the B3 span id must survive")
	assert.Equal(t, "bar", h.Get("Foo"), "the original header must survive")
}

// stubPropagator writes nothing and extracts nothing, used to exercise the
// empty-carrier path independent of any concrete propagator's defaults.
type stubPropagator struct{}

func (stubPropagator) Inject(context.Context, propagation.TextMapCarrier) {}

func (stubPropagator) Fields() []string { return nil }

func (stubPropagator) Extract(ctx context.Context, _ propagation.TextMapCarrier) context.Context {
	return ctx
}

// TestAppendTraceparent_NoTraceContextLeftUntouched: when the propagator writes
// nothing for the context, the original block is left untouched.
func TestAppendTraceparent_NoTraceContextLeftUntouched(t *testing.T) {
	setupTest(t)
	propagator = stubPropagator{}

	assert.Nil(t, appendTraceparent(context.Background(), nil))
}

// TestBeforeSubscribe_ExtractsB3Headers round-trips a B3 trace from the producer
// header block through the wrapped async subscription callback.
func TestBeforeSubscribe_ExtractsB3Headers(t *testing.T) {
	sr := setupTest(t)
	propagator = b3.New()

	ctx, span := tracer.Start(context.Background(), "producer")
	sc := span.SpanContext()
	span.End()
	sr.Reset()

	out := appendTraceparent(ctx, nil)
	require.NotNil(t, out)
	hdr, err := nats.DecodeHeadersMsg(out)
	require.NoError(t, err)

	cb := func(*nats.Msg) {}
	ictx := hooktest.NewMockHookContext(&nats.Conn{}, "orders.created", cb)
	BeforeSubscribe(ictx, &nats.Conn{}, "orders.created", cb)

	wrapped, ok := ictx.GetParam(subscribeCbIndex).(nats.MsgHandler)
	require.True(t, ok)
	wrapped(&nats.Msg{Subject: "orders.created", Header: hdr, Data: []byte("hi")})

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, sc.TraceID(), spans[0].SpanContext().TraceID(),
		"the async consumer must continue the B3 trace")
}

// TestAfterNextMsg_ExtractsB3Headers round-trips a B3 trace from the producer
// header block through the sync NextMsg path.
func TestAfterNextMsg_ExtractsB3Headers(t *testing.T) {
	sr := setupTest(t)
	propagator = b3.New()

	ctx, span := tracer.Start(context.Background(), "producer")
	sc := span.SpanContext()
	span.End()
	sr.Reset()

	out := appendTraceparent(ctx, nil)
	require.NotNil(t, out)
	hdr, err := nats.DecodeHeadersMsg(out)
	require.NoError(t, err)

	msg := &nats.Msg{Subject: "orders.created", Header: hdr, Data: []byte("hi")}
	ictx := hooktest.NewMockHookContext((*nats.Subscription)(nil), msg, nil)
	AfterNextMsg(ictx, msg, nil)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, sc.TraceID(), spans[0].SpanContext().TraceID(),
		"the sync consumer must continue the B3 trace")
}

func TestBeforeSubscribe_WrapsCallback(t *testing.T) {
	sr := setupTest(t)

	called := false
	cb := func(*nats.Msg) { called = true }

	ictx := hooktest.NewMockHookContext(&nats.Conn{}, "orders.created", cb)
	BeforeSubscribe(ictx, &nats.Conn{}, "orders.created", cb)

	wrapped, ok := ictx.GetParam(subscribeCbIndex).(nats.MsgHandler)
	require.True(t, ok, "the callback must be replaced with a wrapped handler")

	msg := &nats.Msg{Subject: "orders.created", Data: []byte("hi")}
	wrapped(msg)

	require.True(t, called, "the original callback must still run")
	spans := sr.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, "orders.created process", spans[0].Name())
	require.Equal(t, trace.SpanKindConsumer, spans[0].SpanKind())
	require.Equal(t, "process", spanAttrs(spans[0])["messaging.operation.type"])
}

func TestBeforeSubscribe_WrappedCallbackSkipsControlSubjects(t *testing.T) {
	sr := setupTest(t)

	called := false
	cb := func(*nats.Msg) { called = true }

	ictx := hooktest.NewMockHookContext(&nats.Conn{}, "orders.created", cb)
	BeforeSubscribe(ictx, &nats.Conn{}, "orders.created", cb)

	wrapped := ictx.GetParam(subscribeCbIndex).(nats.MsgHandler)
	wrapped(&nats.Msg{Subject: "$JS.API.STREAM.CREATE", Data: []byte("hi")})

	require.True(t, called, "the callback must still run for control messages")
	assert.Empty(t, sr.Ended(), "no span for control traffic")
}

func TestBeforeSubscribe_SkipsNilCallback(t *testing.T) {
	setupTest(t)

	ictx := hooktest.NewMockHookContext(&nats.Conn{}, "orders.created", (nats.MsgHandler)(nil))
	BeforeSubscribe(ictx, &nats.Conn{}, "orders.created", (nats.MsgHandler)(nil))

	got := ictx.GetParam(subscribeCbIndex)
	assert.Nil(t, got, "a nil callback must not be wrapped")
}

func TestBeforeQueueSubscribe_WrapsCallback(t *testing.T) {
	sr := setupTest(t)

	called := false
	cb := func(*nats.Msg) { called = true }

	ictx := hooktest.NewMockHookContext(&nats.Conn{}, "orders.created", "workers", cb)
	BeforeQueueSubscribe(ictx, &nats.Conn{}, "orders.created", "workers", cb)

	wrapped, ok := ictx.GetParam(queueSubscribeCbIndex).(nats.MsgHandler)
	require.True(t, ok)
	wrapped(&nats.Msg{Subject: "orders.created", Data: nil})

	require.True(t, called)
	spans := sr.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, "orders.created process", spans[0].Name())
}

func TestAfterNextMsg_RecordsReceiveSpan(t *testing.T) {
	sr := setupTest(t)

	msg := &nats.Msg{Subject: "orders.created", Data: []byte("hi")}
	ictx := hooktest.NewMockHookContext((*nats.Subscription)(nil), msg, nil)
	AfterNextMsg(ictx, msg, nil)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, "orders.created receive", spans[0].Name())
	require.Equal(t, trace.SpanKindConsumer, spans[0].SpanKind())
	require.Equal(t, "receive", spanAttrs(spans[0])["messaging.operation.type"])
	_, hasAddr := spanAttrs(spans[0])["server.address"]
	require.False(t, hasAddr, "a Subscription exposes no server address")
}

func TestAfterNextMsg_Skips(t *testing.T) {
	sr := setupTest(t)

	// No message (timeout): no span.
	ictx := hooktest.NewMockHookContext((*nats.Subscription)(nil), (*nats.Msg)(nil), nats.ErrTimeout)
	AfterNextMsg(ictx, nil, nats.ErrTimeout)

	// Message with error (connection closing): no span.
	msg := &nats.Msg{Subject: "orders.created"}
	ictx = hooktest.NewMockHookContext((*nats.Subscription)(nil), msg, nats.ErrConnectionClosed)
	AfterNextMsg(ictx, msg, nats.ErrConnectionClosed)

	// Control message: no span.
	ictx = hooktest.NewMockHookContext((*nats.Subscription)(nil), &nats.Msg{Subject: "$SYS.REQ.SERVER.PING"}, nil)
	AfterNextMsg(ictx, &nats.Msg{Subject: "$SYS.REQ.SERVER.PING"}, nil)

	assert.Empty(t, sr.Ended())
}
