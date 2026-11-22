// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

func TestSpanName(t *testing.T) {
	tests := []struct {
		name     string
		req      Request
		expected string
	}{
		{
			name: "send subject",
			req: Request{
				Subject:   "orders.created",
				Operation: OperationSend,
			},
			expected: "orders.created send",
		},
		{
			name: "process subject",
			req: Request{
				Subject:   "orders.created",
				Operation: OperationProcess,
			},
			expected: "orders.created process",
		},
		{
			name: "empty subject",
			req: Request{
				Subject:   "",
				Operation: OperationSend,
			},
			expected: "(default) send",
		},
		{
			name: "whitespace subject",
			req: Request{
				Subject:   "   ",
				Operation: OperationProcess,
			},
			expected: "(default) process",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, SpanName(tc.req))
		})
	}
}

func TestTraceAttrs(t *testing.T) {
	t.Run("send with server addr and body size", func(t *testing.T) {
		req := Request{
			Subject:         "user.signup",
			Operation:       OperationSend,
			MessageBodySize: 128,
			ServerAddr:      "127.0.0.1:4222",
		}

		attrs := TraceAttrs(req)
		assert.Contains(t, attrs, semconv.MessagingSystemKey.String("nats"))
		assert.Contains(t, attrs, semconv.MessagingOperationName("send"))
		assert.Contains(t, attrs, semconv.MessagingOperationTypeSend)
		assert.Contains(t, attrs, semconv.MessagingDestinationName("user.signup"))
		assert.Contains(t, attrs, semconv.MessagingMessageBodySize(128))
		assert.Contains(t, attrs, semconv.ServerAddress("127.0.0.1"))
		assert.Contains(t, attrs, semconv.ServerPort(4222))
	})

	t.Run("process with queue subscription", func(t *testing.T) {
		req := Request{
			Subject:         "user.signup",
			Queue:           "workers",
			Operation:       OperationProcess,
			MessageBodySize: 256,
		}

		attrs := TraceAttrs(req)
		assert.Contains(t, attrs, semconv.MessagingSystemKey.String("nats"))
		assert.Contains(t, attrs, semconv.MessagingOperationName("process"))
		assert.Contains(t, attrs, semconv.MessagingOperationTypeProcess)
		assert.Contains(t, attrs, semconv.MessagingDestinationName("user.signup"))
		assert.Contains(t, attrs, semconv.MessagingDestinationSubscriptionName("workers"))
		assert.Contains(t, attrs, semconv.MessagingMessageBodySize(256))
	})

	t.Run("server address without port", func(t *testing.T) {
		req := Request{
			Subject:    "test",
			Operation:  OperationSend,
			ServerAddr: "nats.internal",
		}

		attrs := TraceAttrs(req)
		assert.Contains(t, attrs, attribute.String("server.address", "nats.internal"))
	})
}
