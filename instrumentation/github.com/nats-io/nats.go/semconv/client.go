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

// NatsOperation is the messaging operation recorded on a NATS span.
type NatsOperation string

const (
	// NatsOperationSend is a publish.
	NatsOperationSend NatsOperation = "send"
	// NatsOperationReceive is a message fetched with Subscription.NextMsg.
	NatsOperationReceive NatsOperation = "receive"
	// NatsOperationProcess is a message delivered to an async subscription
	// callback.
	NatsOperationProcess NatsOperation = "process"
)

// Request holds the fields used to build NATS span attributes.
type Request struct {
	// Destination is the subject the message is published to, or the subject a
	// consumed message was published on.
	Destination string
	// Operation is the messaging operation recorded on the span.
	Operation NatsOperation
	// Endpoint is the connected server address in host:port form. It may be
	// empty when the connection does not expose it (e.g. a zero-value Conn in
	// tests, or a Subscription that does not carry its Conn).
	Endpoint string
	// MessageBodySize is the size of the message payload in bytes.
	MessageBodySize int
}

// DestinationName is messaging.destination.name: the subject. An empty subject
// stays empty so SpanName falls back to the bare operation name; an empty
// subject is invalid for publish, but a defensive empty string must not become
// a " (default)"-style invented destination.
func DestinationName(req Request) string {
	return strings.TrimSpace(req.Destination)
}

// SpanName is "{subject} {operation}". The subject is the destination of the
// message: the publish subject on the producer side and the concrete subject
// carried by the message on the consumer side, so a wildcard subscription does
// not collapse every message into one span name.
func SpanName(req Request) string {
	if dest := DestinationName(req); dest != "" {
		return dest + " " + string(req.Operation)
	}
	return string(req.Operation)
}

// TraceAttrs returns NATS messaging attributes. Optional attributes are only
// included when they carry a meaningful value to avoid cluttering spans with
// empty attributes.
func TraceAttrs(req Request) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		// The Go semconv package has no MessagingSystemNATS constant yet;
		// "nats" is the lower-cased system name the messaging semantic
		// conventions use for such cases.
		semconv.MessagingSystemKey.String("nats"),
		semconv.MessagingOperationName(string(req.Operation)),
		semconv.MessagingDestinationName(DestinationName(req)),
	}
	switch req.Operation {
	case NatsOperationSend:
		attrs = append(attrs, semconv.MessagingOperationTypeSend)
	case NatsOperationReceive:
		attrs = append(attrs, semconv.MessagingOperationTypeReceive)
	case NatsOperationProcess:
		attrs = append(attrs, semconv.MessagingOperationTypeProcess)
	}
	if req.Endpoint != "" {
		// ConnectedAddr is host:port (IPv6 literals are bracketed). Fall back to
		// the raw endpoint as server.address if it has no port suffix.
		if host, portStr, err := net.SplitHostPort(req.Endpoint); err == nil {
			attrs = append(attrs, semconv.ServerAddress(host))
			if port, convErr := strconv.Atoi(portStr); convErr == nil && port > 0 {
				attrs = append(attrs, semconv.ServerPort(port))
			}
		} else {
			attrs = append(attrs, semconv.ServerAddress(req.Endpoint))
		}
	}
	if req.MessageBodySize > 0 {
		attrs = append(attrs, semconv.MessagingMessageBodySize(req.MessageBodySize))
	}
	return attrs
}
