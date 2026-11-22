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

// Operation is the messaging operation recorded on a NATS span.
type Operation string

const (
	// OperationSend represents publishing or requesting a message.
	OperationSend Operation = "send"
	// OperationProcess represents a subscriber receiving and handling a message.
	OperationProcess Operation = "process"
)

// DefaultDestination is used when the subject is empty or whitespace.
const DefaultDestination = "(default)"

// Request holds fields used to build NATS span attributes.
type Request struct {
	Subject         string
	Queue           string
	ServerAddr      string
	Operation       Operation
	MessageBodySize int
}

// DestinationName returns the subject or DefaultDestination if empty.
func DestinationName(req Request) string {
	if s := strings.TrimSpace(req.Subject); s != "" {
		return s
	}
	return DefaultDestination
}

// SpanName returns "{destination} {operation}".
func SpanName(req Request) string {
	return DestinationName(req) + " " + string(req.Operation)
}

func serverAttributes(addr string) []attribute.KeyValue {
	trimmed := strings.TrimSpace(addr)
	if trimmed == "" {
		return nil
	}

	host, portStr, err := net.SplitHostPort(trimmed)
	if err != nil {
		return []attribute.KeyValue{semconv.ServerAddress(trimmed)}
	}

	var attrs []attribute.KeyValue
	if host != "" {
		attrs = append(attrs, semconv.ServerAddress(host))
	}
	if port, parseErr := strconv.Atoi(portStr); parseErr == nil && port > 0 {
		attrs = append(attrs, semconv.ServerPort(port))
	}
	return attrs
}

// TraceAttrs returns OpenTelemetry messaging semantic convention attributes for NATS.
func TraceAttrs(req Request) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		semconv.MessagingSystemKey.String("nats"),
		semconv.MessagingOperationName(string(req.Operation)),
		semconv.MessagingDestinationName(DestinationName(req)),
	}

	switch req.Operation {
	case OperationSend:
		attrs = append(attrs, semconv.MessagingOperationTypeSend)
	case OperationProcess:
		attrs = append(attrs, semconv.MessagingOperationTypeProcess)
	}

	if q := strings.TrimSpace(req.Queue); q != "" {
		attrs = append(attrs, semconv.MessagingDestinationSubscriptionName(q))
	}
	if req.MessageBodySize > 0 {
		attrs = append(attrs, semconv.MessagingMessageBodySize(req.MessageBodySize))
	}

	attrs = append(attrs, serverAttributes(req.ServerAddr)...)
	return attrs
}
