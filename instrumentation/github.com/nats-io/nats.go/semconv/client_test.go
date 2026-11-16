// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

func spanAttrMap(attrs []attribute.KeyValue) map[attribute.Key]attribute.Value {
	m := make(map[attribute.Key]attribute.Value, len(attrs))
	for _, a := range attrs {
		m[a.Key] = a.Value
	}
	return m
}

func TestSpanName(t *testing.T) {
	tests := []struct {
		name string
		req  Request
		want string
	}{
		{
			name: "send with subject",
			req:  Request{Destination: "orders.created", Operation: NatsOperationSend},
			want: "orders.created send",
		},
		{
			name: "receive with subject",
			req:  Request{Destination: "orders.created", Operation: NatsOperationReceive},
			want: "orders.created receive",
		},
		{
			name: "process with subject",
			req:  Request{Destination: "orders.created", Operation: NatsOperationProcess},
			want: "orders.created process",
		},
		{
			name: "empty subject falls back to operation only",
			req:  Request{Operation: NatsOperationSend},
			want: "send",
		},
		{
			name: "whitespace subject is trimmed",
			req:  Request{Destination: "  ", Operation: NatsOperationSend},
			want: "send",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, SpanName(tt.req))
		})
	}
}

func TestTraceAttrs(t *testing.T) {
	attrs := TraceAttrs(Request{
		Destination:     "orders.created",
		Operation:       NatsOperationSend,
		Endpoint:        "nats.example.com:4222",
		MessageBodySize: 5,
	})
	m := spanAttrMap(attrs)

	assert.Equal(t, "nats", m["messaging.system"].AsString())
	assert.Equal(t, "send", m["messaging.operation.name"].AsString())
	assert.Equal(t, "send", m["messaging.operation.type"].AsString())
	assert.Equal(t, "orders.created", m["messaging.destination.name"].AsString())
	assert.Equal(t, "nats.example.com", m["server.address"].AsString())
	assert.Equal(t, int64(4222), m["server.port"].AsInt64())
	assert.Equal(t, int64(5), m["messaging.message.body.size"].AsInt64())
}

func TestTraceAttrs_NonDefaultPort(t *testing.T) {
	attrs := TraceAttrs(Request{
		Destination: "orders.created",
		Operation:   NatsOperationProcess,
		Endpoint:    "nats.example.com:6222",
	})
	m := spanAttrMap(attrs)

	assert.Equal(t, "nats.example.com", m["server.address"].AsString())
	assert.Equal(t, int64(6222), m["server.port"].AsInt64())
	assert.Equal(t, "process", m["messaging.operation.type"].AsString())
}

func TestTraceAttrs_EmptyEndpoint(t *testing.T) {
	attrs := TraceAttrs(Request{
		Destination: "orders.created",
		Operation:   NatsOperationReceive,
	})
	m := spanAttrMap(attrs)

	_, hasAddr := m["server.address"]
	assert.False(t, hasAddr, "no server.address when the endpoint is unknown")
	_, hasPort := m["server.port"]
	assert.False(t, hasPort)
}

func TestTraceAttrs_ZeroBodySizeOmitted(t *testing.T) {
	attrs := TraceAttrs(Request{
		Destination: "orders.created",
		Operation:   NatsOperationSend,
	})
	m := spanAttrMap(attrs)

	_, hasSize := m["messaging.message.body.size"]
	assert.False(t, hasSize, "body size 0 (or unset) must not add a size attribute")
}

func TestTraceAttrs_Endpoint(t *testing.T) {
	tests := []struct {
		endpoint string
		wantHost string
		wantPort int64 // 0: no server.port attribute
	}{
		{endpoint: "[::1]:4222", wantHost: "::1", wantPort: 4222},
		{endpoint: "nats", wantHost: "nats"},
		{endpoint: "nats:4x22", wantHost: "nats"},
	}
	for _, tt := range tests {
		t.Run(tt.endpoint, func(t *testing.T) {
			m := spanAttrMap(TraceAttrs(Request{
				Destination: "orders.created",
				Operation:   NatsOperationSend,
				Endpoint:    tt.endpoint,
			}))
			assert.Equal(t, tt.wantHost, m["server.address"].AsString())
			port, hasPort := m["server.port"]
			if tt.wantPort == 0 {
				assert.False(t, hasPort)
				return
			}
			require.True(t, hasPort)
			assert.Equal(t, tt.wantPort, port.AsInt64())
		})
	}
}
