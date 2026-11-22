// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package nats

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	nats "github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	initOnce = sync.Once{}
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

func TestIsEnabled(t *testing.T) {
	t.Run("default enabled", func(t *testing.T) {
		assert.True(t, isEnabled())
	})

	t.Run("disabled via env", func(t *testing.T) {
		t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "nats")
		assert.False(t, isEnabled())
	})

	t.Run("explicitly enabled", func(t *testing.T) {
		t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nats")
		assert.True(t, isEnabled())
	})
}

func TestPublish_Success(t *testing.T) {
	sr := setupTest(t)

	hctx := hooktest.NewMockHookContext()
	BeforePublish(hctx, nil, "orders.new", []byte("payload"))

	span, ok := hctx.GetData().(trace.Span)
	require.True(t, ok)
	require.NotNil(t, span)

	AfterPublish(hctx, nil)

	spans := sr.Ended()
	require.Len(t, spans, 1)

	s := spans[0]
	assert.Equal(t, "orders.new send", s.Name())
	assert.Equal(t, trace.SpanKindProducer, s.SpanKind())
	assert.Equal(t, codes.Unset, s.Status().Code)

	attrs := spanAttrs(s)
	assert.Equal(t, "nats", attrs["messaging.system"])
	assert.Equal(t, "send", attrs["messaging.operation.name"])
	assert.Equal(t, "send", attrs["messaging.operation.type"])
	assert.Equal(t, "orders.new", attrs["messaging.destination.name"])
	assert.Equal(t, int64(7), attrs["messaging.message.body.size"])
}

func TestPublish_Error(t *testing.T) {
	sr := setupTest(t)

	hctx := hooktest.NewMockHookContext()
	BeforePublish(hctx, nil, "orders.new", []byte("payload"))

	testErr := errors.New("publish failed")
	AfterPublish(hctx, testErr)

	spans := sr.Ended()
	require.Len(t, spans, 1)

	s := spans[0]
	assert.Equal(t, codes.Error, s.Status().Code)
	assert.Equal(t, "publish failed", s.Status().Description)
}

func TestPublishMsg_InjectHeaders(t *testing.T) {
	sr := setupTest(t)

	msg := &nats.Msg{
		Subject: "orders.placed",
		Data:    []byte("msg-data"),
	}

	hctx := hooktest.NewMockHookContext(nil, msg)
	BeforePublishMsg(hctx, nil, msg)

	// Verify traceparent injected into msg.Header
	require.NotNil(t, msg.Header)
	tp := msg.Header.Get("traceparent")
	assert.NotEmpty(t, tp)

	AfterPublishMsg(hctx, nil)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, "orders.placed send", spans[0].Name())
}

func TestRequest_Success(t *testing.T) {
	sr := setupTest(t)

	hctx := hooktest.NewMockHookContext()
	BeforeRequest(hctx, nil, "service.ping", []byte("ping"), time.Second)
	AfterRequest(hctx, nil, nil)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, "service.ping send", spans[0].Name())
}

func TestRequestWithContext_Success(t *testing.T) {
	sr := setupTest(t)

	ctx := context.Background()
	hctx := hooktest.NewMockHookContext()
	BeforeRequestWithContext(hctx, nil, ctx, "service.query", []byte("query"))
	AfterRequestWithContext(hctx, nil, nil)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, "service.query send", spans[0].Name())
}

func TestRequestMsg_Success(t *testing.T) {
	sr := setupTest(t)

	msg := nats.NewMsg("service.compute")
	msg.Data = []byte("input")

	hctx := hooktest.NewMockHookContext(nil, msg, time.Second)
	BeforeRequestMsg(hctx, nil, msg, time.Second)

	assert.NotEmpty(t, msg.Header.Get("traceparent"))
	AfterRequestMsg(hctx, nil, nil)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, "service.compute send", spans[0].Name())
}

func TestRequestMsgWithContext_Success(t *testing.T) {
	sr := setupTest(t)

	ctx := context.Background()
	msg := nats.NewMsg("service.fetch")
	msg.Data = []byte("req")

	hctx := hooktest.NewMockHookContext(nil, ctx, msg)
	BeforeRequestMsgWithContext(hctx, nil, ctx, msg)

	assert.NotEmpty(t, msg.Header.Get("traceparent"))
	AfterRequestMsgWithContext(hctx, nil, nil)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, "service.fetch send", spans[0].Name())
}

func TestSubscribe_HandlerWrapped(t *testing.T) {
	sr := setupTest(t)

	var handled bool
	originalHandler := func(msg *nats.Msg) {
		handled = true
		assert.Equal(t, "hello", string(msg.Data))
	}

	hctx := hooktest.NewMockHookContext(nil, "events.user", originalHandler)
	BeforeSubscribe(hctx, nil, "events.user", originalHandler)

	wrapped, ok := hctx.GetParam(subscribeCbParamIndex).(nats.MsgHandler)
	require.True(t, ok)
	require.NotNil(t, wrapped)

	// Simulate incoming message with injected traceparent
	incomingMsg := nats.NewMsg("events.user")
	incomingMsg.Data = []byte("hello")
	incomingMsg.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")

	wrapped(incomingMsg)
	assert.True(t, handled)

	spans := sr.Ended()
	require.Len(t, spans, 1)

	s := spans[0]
	assert.Equal(t, "events.user process", s.Name())
	assert.Equal(t, trace.SpanKindConsumer, s.SpanKind())
	// Verify span has incoming trace ID as parent
	assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", s.SpanContext().TraceID().String())

	attrs := spanAttrs(s)
	assert.Equal(t, "nats", attrs["messaging.system"])
	assert.Equal(t, "process", attrs["messaging.operation.name"])
	assert.Equal(t, "process", attrs["messaging.operation.type"])
	assert.Equal(t, "events.user", attrs["messaging.destination.name"])
}

func TestQueueSubscribe_HandlerWrapped(t *testing.T) {
	sr := setupTest(t)

	var handled bool
	originalHandler := func(msg *nats.Msg) {
		handled = true
	}

	hctx := hooktest.NewMockHookContext(nil, "events.user", "worker-group", originalHandler)
	BeforeQueueSubscribe(hctx, nil, "events.user", "worker-group", originalHandler)

	wrapped, ok := hctx.GetParam(queueSubscribeCbParamIndex).(nats.MsgHandler)
	require.True(t, ok)
	require.NotNil(t, wrapped)

	incomingMsg := &nats.Msg{
		Subject: "events.user",
		Data:    []byte("job"),
	}

	wrapped(incomingMsg)
	assert.True(t, handled)

	spans := sr.Ended()
	require.Len(t, spans, 1)

	s := spans[0]
	assert.Equal(t, "events.user process", s.Name())
	attrs := spanAttrs(s)
	assert.Equal(t, "worker-group", attrs["messaging.destination.subscription.name"])
}

func TestDisabled_NoSpans(t *testing.T) {
	sr := setupTest(t)
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "nats")

	hctx := hooktest.NewMockHookContext()
	BeforePublish(hctx, nil, "orders.new", []byte("payload"))
	AfterPublish(hctx, nil)

	assert.Empty(t, sr.Ended())
}
