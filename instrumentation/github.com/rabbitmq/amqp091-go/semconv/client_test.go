// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
)

func attrMap(attrs []attribute.KeyValue) map[string]attribute.Value {
	m := make(map[string]attribute.Value, len(attrs))
	for _, a := range attrs {
		m[string(a.Key)] = a.Value
	}
	return m
}

func TestSpanName(t *testing.T) {
	tests := []struct {
		name string
		req  Request
		want string
	}{
		{name: "send exchange", req: Request{Exchange: "orders", Operation: OperationSend}, want: "orders send"},
		{
			name: "send default exchange",
			req:  Request{RoutingKey: "orders", Operation: OperationSend},
			want: "(default) send",
		},
		{name: "process queue", req: Request{Queue: "orders", Operation: OperationProcess}, want: "orders process"},
		{name: "receive queue", req: Request{Queue: "orders", Operation: OperationReceive}, want: "orders receive"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, SpanName(tt.req))
		})
	}
}

func TestTraceAttrs_SendOmitsSubscription(t *testing.T) {
	m := attrMap(TraceAttrs(Request{
		Exchange:        "orders",
		RoutingKey:      "order.created",
		Operation:       OperationSend,
		MessageBodySize: 4,
	}))
	assert.Equal(t, "rabbitmq", m["messaging.system"].AsString())
	assert.Equal(t, "send", m["messaging.operation.name"].AsString())
	assert.Equal(t, "send", m["messaging.operation.type"].AsString())
	assert.Equal(t, "orders", m["messaging.destination.name"].AsString())
	assert.Equal(t, "order.created", m["messaging.rabbitmq.destination.routing_key"].AsString())
	assert.Equal(t, int64(4), m["messaging.message.body.size"].AsInt64())
	_, hasSub := m["messaging.destination.subscription.name"]
	assert.False(t, hasSub)
	_, hasAddr := m["server.address"]
	assert.False(t, hasAddr)
}

func TestTraceAttrs_Process(t *testing.T) {
	m := attrMap(TraceAttrs(Request{
		Exchange:   "orders",
		RoutingKey: "order.created",
		Queue:      "orders",
		Operation:  OperationProcess,
	}))
	assert.Equal(t, "process", m["messaging.operation.type"].AsString())
	assert.Equal(t, "orders", m["messaging.destination.name"].AsString())
	assert.Equal(t, "orders", m["messaging.destination.subscription.name"].AsString())
}
