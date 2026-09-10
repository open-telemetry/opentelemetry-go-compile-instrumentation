// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"strings"

	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

// Operation is the messaging operation recorded on a RabbitMQ span.
type Operation string

const (
	// OperationSend is a publish.
	OperationSend Operation = "send"
	// OperationReceive is a delivery that the server already settled (auto-ack).
	OperationReceive Operation = "receive"
	// OperationProcess is a delivery that stays open until Ack, Nack, or Reject.
	OperationProcess Operation = "process"
)

// Request holds the fields used to build RabbitMQ span attributes.
type Request struct {
	Exchange        string
	RoutingKey      string
	Queue           string
	Operation       Operation
	MessageBodySize int
}

// DefaultExchange is the span destination when the publish exchange is empty.
// The default exchange is a real AMQP name (the empty string). Using this
// token keeps the span name low-cardinality and readable.
const DefaultExchange = "(default)"

// DestinationName is messaging.destination.name: the exchange for a send,
// the queue for a receive or process.
func DestinationName(req Request) string {
	if req.Operation == OperationReceive || req.Operation == OperationProcess {
		if q := strings.TrimSpace(req.Queue); q != "" {
			return q
		}
	}
	if ex := strings.TrimSpace(req.Exchange); ex != "" {
		return ex
	}
	return DefaultExchange
}

// SpanName is "{destination} {operation}". The routing key is not in the name.
// Keys such as order.{id} would explode cardinality.
func SpanName(req Request) string {
	return DestinationName(req) + " " + string(req.Operation)
}

// TraceAttrs returns RabbitMQ messaging attributes. server.address is omitted:
// (*Channel).connection is unexported, so the broker host is not available
// at the hook.
func TraceAttrs(req Request) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		semconv.MessagingSystemRabbitMQ,
		semconv.MessagingOperationName(string(req.Operation)),
		semconv.MessagingDestinationName(DestinationName(req)),
	}
	switch req.Operation {
	case OperationSend:
		attrs = append(attrs, semconv.MessagingOperationTypeSend)
	case OperationReceive:
		attrs = append(attrs, semconv.MessagingOperationTypeReceive)
	case OperationProcess:
		attrs = append(attrs, semconv.MessagingOperationTypeProcess)
	}
	if key := strings.TrimSpace(req.RoutingKey); key != "" {
		attrs = append(attrs, semconv.MessagingRabbitMQDestinationRoutingKey(key))
	}
	if q := strings.TrimSpace(req.Queue); q != "" && req.Operation != OperationSend {
		attrs = append(attrs, semconv.MessagingDestinationSubscriptionName(q))
	}
	if req.MessageBodySize > 0 {
		attrs = append(attrs, semconv.MessagingMessageBodySize(req.MessageBodySize))
	}
	return attrs
}
