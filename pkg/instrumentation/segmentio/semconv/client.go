// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"net"
	"strconv"

	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

type (
	KafkaOperation   string
	KafkaDestination string
)

const (
	KafkaDestinationTopic KafkaDestination = "topic"
	KafkaDestinationQueue KafkaDestination = "queue"
)

const (
	KafkaOperationReceive KafkaOperation = "receive"
	KafkaOperationProcess KafkaOperation = "process"
)

type KafkaRequest struct {
	EndPoint        string
	Destination     KafkaDestination
	Operation       KafkaOperation
	Partition       string
	Offset          int
	ConsumerGroupID string
	MessageKey      string
}

func KafkaRequestTraceAttrs(req KafkaRequest) []attribute.KeyValue {
	host, portStr, err := net.SplitHostPort(req.EndPoint)
	if err != nil {
		host = req.EndPoint
	}
	attrs := []attribute.KeyValue{
		semconv.MessagingSystemKafka,
		semconv.ServerAddress(host),
		semconv.MessagingDestinationPartitionID(req.Partition),
		semconv.MessagingOperationName(string(req.Operation)),
		semconv.MessagingDestinationName(string(req.Destination)),
		semconv.MessagingKafkaOffset(req.Offset),
		semconv.MessagingConsumerGroupName(req.ConsumerGroupID),
		semconv.MessagingKafkaMessageKey(req.MessageKey),
	}
	if err == nil {
		if port, convErr := strconv.Atoi(portStr); convErr == nil && port > 0 {
			attrs = append(attrs, semconv.ServerPort(port))
		}
	}
	return attrs
}
