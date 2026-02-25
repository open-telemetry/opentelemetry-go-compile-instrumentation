// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

type KafkaOperation string

const (
	KafkaOperationPublish KafkaOperation = "publish"
	KafkaOperationReceive KafkaOperation = "receive"
	KafkaOperationProcess KafkaOperation = "process"
	KafkaOperationAck     KafkaOperation = "ack"
)

type KafkaRequest struct {
	Topic           string
	Operation       KafkaOperation
	Partition       string
	Offset          int
	ConsumerGroupID string
	MessageKey      string
}

func KafkaRequestTraceAttrs(req KafkaRequest) []attribute.KeyValue {
	return []attribute.KeyValue{
		semconv.MessagingSystemKafka,
		semconv.MessagingDestinationName(req.Topic),
		semconv.MessagingDestinationPartitionIDKey.String(req.Partition),
		semconv.MessagingOperationName(string(req.Operation)),
		semconv.MessagingKafkaOffset(req.Offset),
		semconv.MessagingConsumerGroupName(req.ConsumerGroupID),
		semconv.MessagingKafkaMessageKey(req.MessageKey),
	}
}
