// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"net"
	"reflect"
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
// attributes for a single IBM/sarama Kafka operation.
//
// The `OnSend` and `OnConsume` interceptor callbacks used here cannot observe
// broker acknowledgment or application-handler completion, so spans are
// created and ended inside those callbacks. Async marks the producer span as a
// local hand-off span rather than a broker round-trip.
type KafkaRequest struct {
	// Endpoint is the broker address in host:port form. It may be empty.
	Endpoint string
	// Destination is the Kafka topic.
	Destination string
	// Operation is the messaging operation (send or receive).
	Operation KafkaOperation
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
	// Async indicates whether the producer is an asynchronous one (no
	// Produce ack wait in the application path). Sync producers omit the
	// messaging.kafka.async attribute entirely.
	Async bool

	// ConsumerGroup is the consumer group name, when the consumer belongs to
	// one. Empty for plain partition consumers.
	ConsumerGroup string
}

// KafkaMessageKey returns a Kafka message key as a valid UTF-8 string.
// Invalid UTF-8 sequences are replaced with the Unicode replacement character.
// messagingKafkaAsyncKey marks spans for asynchronous producers. It is a
// sarama-specific hint (not part of the stable messaging conventions) and is
// emitted only when true; sync producers omit it.
const messagingKafkaAsyncKey = attribute.Key("messaging.kafka.async")

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
	if req.ConsumerGroup != "" {
		attrs = append(attrs, semconv.MessagingConsumerGroupName(req.ConsumerGroup))
	}

	return attrs
}

// MessagingErrorType returns the error.type attribute for a failed send.
func MessagingErrorType(err error) attribute.KeyValue {
	if err == nil {
		return semconv.ErrorTypeKey.String("")
	}
	return semconv.ErrorTypeKey.String(errorTypeName(err))
}

func errorTypeName(err error) string {
	t := reflect.TypeOf(err)
	if t == nil {
		return ""
	}
	if t.PkgPath() == "" {
		return t.String()
	}
	name := t.Name()
	if name == "" {
		return t.String()
	}
	return name
}
