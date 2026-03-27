// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
package semconv

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKafkaClientRequestTraceAttrs(t *testing.T) {
	tests := []struct {
		name     string
		req      KafkaRequest
		expected map[string]interface{}
	}{
		{
			name: "basic PRODUCE request to TOPIC",
			req: KafkaRequest{
				EndPoint:    "localhost:9092",
				Destination: KafkaDestinationTopic,
				Operation:   KafkaOperationProcess,
				MessageKey:  "order-12",
			},
			expected: map[string]interface{}{
				"messaging.system.name":       "kafka",
				"server.address":              "localhost",
				"server.port":                 int64(9092),
				"messaging.destination.name":  string(KafkaDestinationTopic),
				"messaging.operation.name":    "process",
				"messaging.kafka.message_key": "order-12",
			},
		},
		{
			name: "basic PRODUCE request to QUEUE",
			req: KafkaRequest{
				EndPoint:    "localhost:9092",
				Destination: KafkaDestinationQueue,
				Operation:   KafkaOperationProcess,
				MessageKey:  "order-13",
			},
			expected: map[string]interface{}{
				"messaging.system.name":       "kafka",
				"server.address":              "localhost",
				"server.port":                 int64(9092),
				"messaging.destination.name":  string(KafkaDestinationQueue),
				"messaging.operation.name":    "process",
				"messaging.kafka.message_key": "order-13",
			},
		},
		{
			name: "RECEIVE operation",
			req: KafkaRequest{
				EndPoint:    "localhost:9092",
				Destination: KafkaDestinationTopic,
				Operation:   KafkaOperationReceive,
				MessageKey:  "order-99",
			},
			expected: map[string]interface{}{
				"messaging.system.name":       "kafka",
				"server.address":              "localhost",
				"server.port":                 int64(9092),
				"messaging.destination.name":  string(KafkaDestinationTopic),
				"messaging.operation.name":    "receive",
				"messaging.kafka.message_key": "order-99",
			},
		},
		{
			name: "request with partition and offset",
			req: KafkaRequest{
				EndPoint:    "localhost:9092",
				Destination: KafkaDestinationTopic,
				Operation:   KafkaOperationReceive,
				Partition:   "3",
				Offset:      42,
				MessageKey:  "order-55",
			},
			expected: map[string]interface{}{
				"messaging.system.name":       "kafka",
				"server.address":              "localhost",
				"server.port":                 int64(9092),
				"messaging.destination.name":  string(KafkaDestinationTopic),
				"messaging.operation.name":    "receive",
				"messaging.kafka.message_key": "order-55",
				"messaging.kafka.partition":   "3",
				"messaging.kafka.offset":      42,
			},
		},
		{
			name: "empty fields",
			req: KafkaRequest{
				EndPoint:    "",
				Destination: "",
				Operation:   "",
				MessageKey:  "",
			},
			expected: map[string]interface{}{
				"messaging.system.name":       "kafka",
				"server.address":              "",
				"messaging.destination.name":  "",
				"messaging.operation.name":    "",
				"messaging.kafka.message_key": "",
			},
		},
		{
			name: "endpoint without port",
			req: KafkaRequest{
				EndPoint: "localhost",
			},
			expected: map[string]interface{}{
				"messaging.system.name": "kafka",
				"server.address":        "localhost",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := KafkaRequestTraceAttrs(tt.req)

			attrMap := make(map[string]interface{})
			for _, attr := range attrs {
				attrMap[string(attr.Key)] = attr.Value.AsInterface()
			}

			require.Len(t, attrMap, len(tt.expected), "attribute count mismatch")

			for key, expectedVal := range tt.expected {
				actualVal, ok := attrMap[key]
				require.True(t, ok, "expected attribute %s not found", key)
				assert.Equal(t, expectedVal, actualVal, "attribute %s value mismatch", key)
			}
		})
	}
}
