// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"strconv"

	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

type KafkaOperation string

const (
	KafkaOperationPublish KafkaOperation = "publish"
	KafkaOperationReceive KafkaOperation = "receive"
	KafkaOperationProcess KafkaOperation = "process"
	KafkaOperationSettle  KafkaOperation = "settle"
)

type KafkaRequest struct {
	Topic           string
	Operation       KafkaOperation
	Partition       string
	Offset          string
	ConsumerGroupID string
	MessageKey      string
}

func KafkaRequestTraceAttrs(req KafkaRequest) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		semconv.MessagingSystemKafka,
		semconv.MessagingDestinationName(req.Topic),
		semconv.MessagingDestinationPartitionIDKey.String(req.Partition),
		semconv.MessagingOperationName(string(req.Operation)),
	}

	if req.MessageKey != "" {
		attrs = append(attrs, semconv.MessagingKafkaMessageKey(req.MessageKey))
	}

	isConsumer := req.Operation == KafkaOperationReceive ||
		req.Operation == KafkaOperationProcess ||
		req.Operation == KafkaOperationSettle ||
		req.Operation == KafkaOperationPublish

	if isConsumer {
		if req.Offset != "" {
			if offset, err := strconv.Atoi(req.Offset); err == nil {
				attrs = append(attrs, semconv.MessagingKafkaOffset(offset))
			}
		}
		if req.ConsumerGroupID != "" {
			attrs = append(attrs, semconv.MessagingConsumerGroupName(req.ConsumerGroupID))
		}
	}

	return attrs
}
