// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package amqp091

import (
	"context"
	"errors"
	"sync"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	amqprop "go.opentelemetry.io/otelc/instrumentation/github.com/rabbitmq/amqp091-go/internal/propagation"
	"go.opentelemetry.io/otelc/pkg/hook/hooktest"
)

func setupTest(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "amqp")

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

func TestAmqpEnabler(t *testing.T) {
	tests := []struct {
		name     string
		setupEnv func(t *testing.T)
		want     bool
	}{
		{
			name: "enabled explicitly",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "amqp")
			},
			want: true,
		},
		{
			name: "disabled explicitly",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "amqp")
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

func TestBeforeAfterPublish_InjectsHeadersAndEndsSpan(t *testing.T) {
	sr := setupTest(t)

	msg := amqp.Publishing{Body: []byte("hello")}
	ictx := hooktest.NewMockHookContext((*amqp.Channel)(nil), "orders", "order.created", false, false, msg)
	BeforePublishWithDeferredConfirm(ictx, nil, "orders", "order.created", false, false, msg)

	written, ok := ictx.GetParam(publishMsgIndex).(amqp.Publishing)
	require.True(t, ok)
	require.NotEmpty(t, amqprop.NewTableCarrier(&written.Headers).Get("traceparent"))

	AfterPublishWithDeferredConfirm(ictx, nil, nil)
	spans := sr.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, "orders send", spans[0].Name())
	require.Equal(t, trace.SpanKindProducer, spans[0].SpanKind())
	require.Equal(t, codes.Unset, spans[0].Status().Code)
	attrs := spanAttrs(spans[0])
	require.Equal(t, "rabbitmq", attrs["messaging.system"])
	require.Equal(t, "send", attrs["messaging.operation.name"])
	require.Equal(t, "send", attrs["messaging.operation.type"])
	require.Equal(t, "orders", attrs["messaging.destination.name"])
	require.Equal(t, "order.created", attrs["messaging.rabbitmq.destination.routing_key"])
	_, hasAddr := attrs["server.address"]
	require.False(t, hasAddr, "server.address is not available at the hook")
}

func TestAfterPublish_RecordsError(t *testing.T) {
	sr := setupTest(t)

	msg := amqp.Publishing{Body: []byte("hello")}
	ictx := hooktest.NewMockHookContext((*amqp.Channel)(nil), "", "orders", false, false, msg)
	BeforePublishWithDeferredConfirm(ictx, nil, "", "orders", false, false, msg)
	AfterPublishWithDeferredConfirm(ictx, nil, errors.New("channel closed"))

	spans := sr.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, "(default) send", spans[0].Name())
	require.Equal(t, codes.Error, spans[0].Status().Code)
}

func TestPublish_DisabledSkipsSpan(t *testing.T) {
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "amqp")
	sr := setupTest(t)
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "amqp")

	msg := amqp.Publishing{Body: []byte("hello")}
	ictx := hooktest.NewMockHookContext((*amqp.Channel)(nil), "orders", "k", false, false, msg)
	BeforePublishWithDeferredConfirm(ictx, nil, "orders", "k", false, false, msg)
	AfterPublishWithDeferredConfirm(ictx, nil, nil)
	require.Empty(t, sr.Ended())
}

type stubAck struct {
	ackErr error
}

func (s stubAck) Ack(uint64, bool) error { return s.ackErr }

func (s stubAck) Nack(uint64, bool, bool) error { return s.ackErr }

func (s stubAck) Reject(uint64, bool) error { return s.ackErr }

func TestWrapDeliveries_ProcessEndsOnAck(t *testing.T) {
	sr := setupTest(t)

	in := make(chan amqp.Delivery, 1)
	out := wrapDeliveries("orders", false, in)
	in <- amqp.Delivery{
		Acknowledger: stubAck{},
		DeliveryTag:  1,
		RoutingKey:   "order.created",
		Body:         []byte("hi"),
	}

	d := <-out
	require.Empty(t, sr.Ended(), "process span must stay open until Ack")
	require.NoError(t, d.Ack(false))
	close(in)
	_, ok := <-out
	require.False(t, ok)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, "orders process", spans[0].Name())
	require.Equal(t, trace.SpanKindConsumer, spans[0].SpanKind())
	require.Equal(t, codes.Unset, spans[0].Status().Code)
	attrs := spanAttrs(spans[0])
	require.Equal(t, "process", attrs["messaging.operation.type"])
	require.Equal(t, "orders", attrs["messaging.destination.subscription.name"])
}

func TestWrapDeliveries_AutoAckEndsImmediately(t *testing.T) {
	sr := setupTest(t)

	in := make(chan amqp.Delivery, 1)
	out := wrapDeliveries("orders", true, in)
	in <- amqp.Delivery{Acknowledger: stubAck{}, Body: []byte("hi")}
	close(in)
	<-out
	_, ok := <-out
	require.False(t, ok)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, "orders receive", spans[0].Name())
	require.Equal(t, "receive", spanAttrs(spans[0])["messaging.operation.type"])
}

func TestWrapDeliveries_NackSetsError(t *testing.T) {
	sr := setupTest(t)

	in := make(chan amqp.Delivery, 1)
	out := wrapDeliveries("orders", false, in)
	in <- amqp.Delivery{Acknowledger: stubAck{}, DeliveryTag: 1}
	d := <-out
	require.NoError(t, d.Nack(false, false))
	close(in)
	<-out

	spans := sr.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Error, spans[0].Status().Code)
	require.Equal(t, "nack", spans[0].Status().Description)
}

func TestWrapDeliveries_ClosedChannelEndsLeftover(t *testing.T) {
	sr := setupTest(t)

	in := make(chan amqp.Delivery, 1)
	out := wrapDeliveries("orders", false, in)
	in <- amqp.Delivery{Acknowledger: stubAck{}, DeliveryTag: 1}
	close(in)
	<-out
	_, ok := <-out
	require.False(t, ok)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Unset, spans[0].Status().Code)
}

func TestAfterConsume_WrapsChannel(t *testing.T) {
	setupTest(t)

	in := make(chan amqp.Delivery)
	close(in)
	ictx := hooktest.NewMockHookContext()
	ictx.SetData(&consumeData{queue: "orders", autoAck: true})
	AfterConsume(ictx, in, nil)
	wrapped, ok := ictx.GetReturnVal(0).(<-chan amqp.Delivery)
	require.True(t, ok)
	_, open := <-wrapped
	require.False(t, open)
}

func TestAfterGet_WrapsDelivery(t *testing.T) {
	sr := setupTest(t)

	ictx := hooktest.NewMockHookContext()
	ictx.SetData(&consumeData{queue: "orders", autoAck: false})
	msg := amqp.Delivery{Acknowledger: stubAck{}, Body: []byte("hi")}
	AfterGet(ictx, msg, true, nil)
	got, ok := ictx.GetReturnVal(0).(amqp.Delivery)
	require.True(t, ok)
	require.NoError(t, got.Ack(false))
	require.Len(t, sr.Ended(), 1)
}

func TestAfterConsume_ErrorSkipsWrap(t *testing.T) {
	setupTest(t)

	in := make(chan amqp.Delivery)
	ictx := hooktest.NewMockHookContext()
	ictx.SetData(&consumeData{queue: "orders", autoAck: true})
	AfterConsume(ictx, in, errors.New("consume failed"))
	require.Nil(t, ictx.GetReturnVal(0))
}

func TestAfterGet_EmptyQueueSkips(t *testing.T) {
	setupTest(t)

	ictx := hooktest.NewMockHookContext()
	ictx.SetData(&consumeData{queue: "orders", autoAck: false})
	AfterGet(ictx, amqp.Delivery{}, false, nil)
	require.Nil(t, ictx.GetReturnVal(0))
}

func TestBeforeConsumeWithContext_StoresQueue(t *testing.T) {
	setupTest(t)

	ictx := hooktest.NewMockHookContext()
	BeforeConsumeWithContext(ictx, nil, context.Background(), "orders", "", false, false, false, false, nil)
	data, ok := ictx.GetData().(*consumeData)
	require.True(t, ok)
	require.Equal(t, "orders", data.queue)
	require.False(t, data.autoAck)
}

func TestWrapDeliveries_RejectSetsError(t *testing.T) {
	sr := setupTest(t)

	in := make(chan amqp.Delivery, 1)
	out := wrapDeliveries("orders", false, in)
	in <- amqp.Delivery{Acknowledger: stubAck{}, DeliveryTag: 1}
	d := <-out
	require.NoError(t, d.Reject(false))
	close(in)
	<-out

	spans := sr.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Error, spans[0].Status().Code)
	require.Equal(t, "reject", spans[0].Status().Description)
}

func TestAfterPublish_NoBeforeIsNoop(t *testing.T) {
	sr := setupTest(t)
	AfterPublishWithDeferredConfirm(hooktest.NewMockHookContext(), nil, errors.New("unused"))
	require.Empty(t, sr.Ended())
}

func TestPublishLinksToConsumeViaHeaders(t *testing.T) {
	sr := setupTest(t)

	msg := amqp.Publishing{Body: []byte("hello")}
	pub := hooktest.NewMockHookContext((*amqp.Channel)(nil), "ex", "rk", false, false, msg)
	BeforePublishWithDeferredConfirm(pub, nil, "ex", "rk", false, false, msg)
	injected := pub.GetParam(publishMsgIndex).(amqp.Publishing)
	AfterPublishWithDeferredConfirm(pub, nil, nil)

	in := make(chan amqp.Delivery, 1)
	out := wrapDeliveries("orders", true, in)
	in <- amqp.Delivery{Headers: injected.Headers, Body: msg.Body}
	close(in)
	<-out
	_, open := <-out
	require.False(t, open)

	spans := sr.Ended()
	require.Len(t, spans, 2)
	require.Equal(t, spans[0].SpanContext().TraceID(), spans[1].SpanContext().TraceID())
	require.Equal(t, spans[0].SpanContext().SpanID(), spans[1].Parent().SpanID())
}
