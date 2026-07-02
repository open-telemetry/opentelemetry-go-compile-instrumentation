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

	// Consumer-only and offset/partition attrs must be absent on the producer side.
	_, hasGroup := m["messaging.consumer.group.name"]
	assert.False(t, hasGroup)
	_, hasPartition := m["messaging.destination.partition.id"]
	assert.False(t, hasPartition)
}

func TestKafkaRequestTraceAttrs_Consumer(t *testing.T) {
	attrs := KafkaRequestTraceAttrs(KafkaRequest{
		Endpoint:        "localhost:9092",
		Destination:     "orders",
		Operation:       KafkaOperationReceive,
		ConsumerGroupID: "workers",
		MessageKey:      "order-1",
		MessageBodySize: 64,
		Partition:       0,
		Offset:          0,
		HasPartition:    true,
		HasOffset:       true,
	})
	m := attrMap(attrs)

	assert.Equal(t, "kafka", m["messaging.system"].AsString())
	assert.Equal(t, "receive", m["messaging.operation.name"].AsString())
	assert.Equal(t, "receive", m["messaging.operation.type"].AsString())
	assert.Equal(t, "workers", m["messaging.consumer.group.name"].AsString())
	// Partition 0 and offset 0 are valid values and must be emitted when the
	// Has* flags are set.
	assert.Equal(t, "0", m["messaging.destination.partition.id"].AsString())
	assert.Equal(t, int64(0), m["messaging.kafka.offset"].AsInt64())
}

func TestKafkaRequestTraceAttrs_OmitsEmptyOptionals(t *testing.T) {
	attrs := KafkaRequestTraceAttrs(KafkaRequest{
		Destination: "orders",
		Operation:   KafkaOperationReceive,
	})
	m := attrMap(attrs)

	for _, key := range []string{
		"server.address",
		"server.port",
		"messaging.consumer.group.name",
		"messaging.kafka.message.key",
		"messaging.message.body.size",
		"messaging.destination.partition.id",
		"messaging.kafka.offset",
	} {
		_, ok := m[key]
		assert.Falsef(t, ok, "expected %q to be omitted", key)
	}
}

func TestKafkaRequestTraceAttrs_EndpointWithoutPort(t *testing.T) {
	attrs := KafkaRequestTraceAttrs(KafkaRequest{
		Endpoint:    "broker-only-host",
		Destination: "orders",
		Operation:   KafkaOperationSend,
	})
	m := attrMap(attrs)

	// When the endpoint has no port, the whole value is used as server.address
	// and no server.port is emitted.
	assert.Equal(t, "broker-only-host", m["server.address"].AsString())
	_, hasPort := m["server.port"]
	assert.False(t, hasPort)
}
