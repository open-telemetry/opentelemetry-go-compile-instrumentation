// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"net"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

// KafkaOperation identifies the messaging operation performed by the client.
type KafkaOperation string

const (
	// KafkaOperationSend is the operation name for producing messages.
	KafkaOperationSend KafkaOperation = "send"
	// KafkaOperationReceive is the operation name for consuming messages.
	KafkaOperationReceive KafkaOperation = "receive"
)

// KafkaRequest carries the information needed to build the semantic convention
// attributes for a single Kafka client operation.
type KafkaRequest struct {
	// Endpoint is the broker address in host:port form. It may be empty.
	Endpoint string
	// Destination is the Kafka topic.
	Destination string
	// Operation is the messaging operation (send or receive).
	Operation KafkaOperation
	// ConsumerGroupID is the consumer group name (consumer side only).
	ConsumerGroupID string
	// MessageKey is the Kafka message key, when present.
	MessageKey string
	// MessageBodySize is the size of the message value in bytes.
	MessageBodySize int
	// Partition is the Kafka partition. Only emitted when HasPartition is true,
	// because partition 0 is a valid value.
	Partition int
	// Offset is the Kafka offset. Only emitted when HasOffset is true, because
	// offset 0 is a valid value.
	Offset int64
	// HasPartition indicates whether Partition holds a meaningful value.
	HasPartition bool
	// HasOffset indicates whether Offset holds a meaningful value.
	HasOffset bool
	// Async indicates the producer span covers a fire-and-forget write to a
	// kafka.Writer with Async enabled: the span only reflects local hand-off to
	// the client library, not confirmed delivery, so its duration and status
	// must not be read as broker round-trip latency or delivery success.
	Async bool
}

// messagingKafkaAsyncKey has no OpenTelemetry semantic convention yet; it marks
// producer spans for kafka.Writer.Async writes so consumers of the trace data
// don't mistake enqueue-time duration and Unset/Ok status for confirmed
// delivery (see KafkaRequest.Async).
//
// Deliberately emitted only as `true`, never as an explicit `false` for the
// synchronous path: keeping the attribute absent for the (overwhelmingly more
// common) sync case avoids adding an attribute to every producer span just to
// say "this one is trustworthy", which is the default assumption anyway.
const messagingKafkaAsyncKey = attribute.Key("messaging.kafka.async")

// KafkaMessageKey returns a Kafka message key as a valid UTF-8 string.
// Invalid UTF-8 sequences are replaced with the Unicode replacement character.
func KafkaMessageKey(key []byte) string {
	return strings.ToValidUTF8(string(key), "\uFFFD")
}

// KafkaRequestTraceAttrs returns the trace attributes for a Kafka client
// operation. Optional attributes are only included when they carry a
// meaningful value to avoid cluttering spans with empty attributes.
func KafkaRequestTraceAttrs(req KafkaRequest) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		semconv.MessagingSystemKafka,
		semconv.MessagingOperationName(string(req.Operation)),
		semconv.MessagingDestinationName(req.Destination),
	}

	switch req.Operation {
	case KafkaOperationSend:
		attrs = append(attrs, semconv.MessagingOperationTypeSend)
		if req.Async {
			attrs = append(attrs, messagingKafkaAsyncKey.Bool(true))
		}
	case KafkaOperationReceive:
		attrs = append(attrs, semconv.MessagingOperationTypeReceive)
	}

	if req.Endpoint != "" {
		host, portStr, err := net.SplitHostPort(req.Endpoint)
		if err != nil {
			attrs = append(attrs, semconv.ServerAddress(req.Endpoint))
		} else {
			attrs = append(attrs, semconv.ServerAddress(host))
			if port, convErr := strconv.Atoi(portStr); convErr == nil && port > 0 {
				attrs = append(attrs, semconv.ServerPort(port))
			}
		}
	}

	if req.ConsumerGroupID != "" {
		attrs = append(attrs, semconv.MessagingConsumerGroupName(req.ConsumerGroupID))
	}
	if req.MessageKey != "" {
		attrs = append(attrs, semconv.MessagingKafkaMessageKey(req.MessageKey))
	}
	if req.MessageBodySize > 0 {
		attrs = append(attrs, semconv.MessagingMessageBodySize(req.MessageBodySize))
	}
	if req.HasPartition {
		attrs = append(attrs, semconv.MessagingDestinationPartitionID(strconv.Itoa(req.Partition)))
	}
	if req.HasOffset {
		attrs = append(attrs, semconv.MessagingKafkaOffset(int(req.Offset)))
	}

	return attrs
}
