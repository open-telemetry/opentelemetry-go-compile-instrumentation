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

func TestKafkaMessageKey(t *testing.T) {
	tests := []struct {
		name     string
		key      []byte
		expected string
	}{
		{name: "ascii", key: []byte("order-1"), expected: "order-1"},
		{name: "utf8", key: []byte("\u20ac"), expected: "\u20ac"},
		{name: "empty", key: []byte{}, expected: ""},
		{name: "invalid utf8", key: []byte{0xff, 0xfe, 0xfd}, expected: "\uFFFD"},
		{name: "invalid utf8 preserves valid text", key: []byte{'o', 0xff, 'k'}, expected: "o\uFFFDk"},
		{name: "null byte", key: []byte{0x00}, expected: "\x00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, KafkaMessageKey(tt.key))
		})
	}
}

func TestKafkaRequestTraceAttrs_Producer(t *testing.T) {
	attrs := KafkaRequestTraceAttrs(KafkaRequest{
		Endpoint:        "broker.example.com:9092",
		Destination:     "orders",
		Operation:       KafkaOperationSend,
		MessageKey:      "order-1",
		MessageBodySize: 128,
	})
	m := attrMap(attrs)

	assert.Equal(t, "kafka", m["messaging.system"].AsString())
	assert.Equal(t, "send", m["messaging.operation.name"].AsString())
	assert.Equal(t, "send", m["messaging.operation.type"].AsString())
	assert.Equal(t, "orders", m["messaging.destination.name"].AsString())
	assert.Equal(t, "broker.example.com", m["server.address"].AsString())
	assert.Equal(t, int64(9092), m["server.port"].AsInt64())
	assert.Equal(t, "order-1", m["messaging.kafka.message.key"].AsString())
	assert.Equal(t, int64(128), m["messaging.message.body.size"].AsInt64())

	// Offset/partition attrs must be absent on the producer side.
	_, hasPartition := m["messaging.destination.partition.id"]
	assert.False(t, hasPartition)
	_, hasOffset := m["messaging.kafka.offset"]
	assert.False(t, hasOffset)
}

func TestKafkaRequestTraceAttrs_ProducerAsync(t *testing.T) {
	attrs := KafkaRequestTraceAttrs(KafkaRequest{
		Destination: "orders",
		Operation:   KafkaOperationSend,
		Async:       true,
	})
	m := attrMap(attrs)

	assert.True(t, m["messaging.kafka.async"].AsBool())
}

func TestKafkaRequestTraceAttrs_ProducerSyncOmitsAsync(t *testing.T) {
	attrs := KafkaRequestTraceAttrs(KafkaRequest{
		Destination: "orders",
		Operation:   KafkaOperationSend,
		Async:       false,
	})
	m := attrMap(attrs)

	_, hasAsync := m["messaging.kafka.async"]
	assert.False(t, hasAsync)
}

func TestKafkaRequestTraceAttrs_ConsumerIgnoresAsync(t *testing.T) {
	// Async only has meaning for the producer (send) side; it must never be
	// emitted for a receive operation even if the field is mistakenly set.
	attrs := KafkaRequestTraceAttrs(KafkaRequest{
		Destination: "orders",
		Operation:   KafkaOperationReceive,
		Async:       true,
	})
	m := attrMap(attrs)

	_, hasAsync := m["messaging.kafka.async"]
	assert.False(t, hasAsync)
}

func TestKafkaRequestTraceAttrs_Consumer(t *testing.T) {
	attrs := KafkaRequestTraceAttrs(KafkaRequest{
		Endpoint:        "localhost:9092",
		Destination:     "orders",
		Operation:       KafkaOperationReceive,
		MessageKey:      "order-1",
		MessageBodySize: 128,
		Partition:       2,
		HasPartition:    true,
		Offset:          42,
		HasOffset:       true,
	})
	m := attrMap(attrs)

	assert.Equal(t, "kafka", m["messaging.system"].AsString())
	assert.Equal(t, "receive", m["messaging.operation.name"].AsString())
	assert.Equal(t, "receive", m["messaging.operation.type"].AsString())
	assert.Equal(t, "orders", m["messaging.destination.name"].AsString())
	assert.Equal(t, "localhost", m["server.address"].AsString())
	assert.Equal(t, int64(9092), m["server.port"].AsInt64())
	assert.Equal(t, "2", m["messaging.destination.partition.id"].AsString())
	assert.Equal(t, int64(42), m["messaging.kafka.offset"].AsInt64())
}

func TestKafkaRequestTraceAttrs_EndpointInvalid(t *testing.T) {
	// A broker address without a port is emitted as server.address verbatim.
	attrs := KafkaRequestTraceAttrs(KafkaRequest{
		Endpoint:    "broker.example.com",
		Destination: "orders",
		Operation:   KafkaOperationReceive,
	})
	m := attrMap(attrs)

	assert.Equal(t, "broker.example.com", m["server.address"].AsString())
	_, hasPort := m["server.port"]
	assert.False(t, hasPort)
}

func TestKafkaRequestTraceAttrs_PartitionZero(t *testing.T) {
	// Partition 0 is a valid value and must be emitted when HasPartition is
	// true; without the flag it must stay absent.
	withFlag := KafkaRequestTraceAttrs(KafkaRequest{
		Destination:  "orders",
		Operation:    KafkaOperationReceive,
		Partition:    0,
		HasPartition: true,
	})
	assert.Equal(t, "0", attrMap(withFlag)["messaging.destination.partition.id"].AsString())

	withoutFlag := KafkaRequestTraceAttrs(KafkaRequest{
		Destination: "orders",
		Operation:   KafkaOperationReceive,
	})
	_, hasPartition := attrMap(withoutFlag)["messaging.destination.partition.id"]
	assert.False(t, hasPartition)
}

func TestKafkaRequestTraceAttrs_OmitsEmptyOptional(t *testing.T) {
	attrs := KafkaRequestTraceAttrs(KafkaRequest{
		Destination: "orders",
		Operation:   KafkaOperationSend,
	})
	m := attrMap(attrs)

	// No endpoint, key, body size, partition or offset when not provided.
	_, hasServer := m["server.address"]
	assert.False(t, hasServer)
	_, hasKey := m["messaging.kafka.message.key"]
	assert.False(t, hasKey)
	_, hasSize := m["messaging.message.body.size"]
	assert.False(t, hasSize)
}
