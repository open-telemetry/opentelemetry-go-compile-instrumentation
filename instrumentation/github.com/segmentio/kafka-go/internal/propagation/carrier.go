// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package propagation provides shared utilities for trace context propagation
// through Kafka message headers.
package propagation

import (
	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel/propagation"
)

// Compile-time assertion that HeaderCarrier implements propagation.TextMapCarrier.
var _ propagation.TextMapCarrier = HeaderCarrier{}

// HeaderCarrier adapts Kafka message headers to the OpenTelemetry TextMapCarrier
// interface so trace context can be propagated through Kafka messages.
type HeaderCarrier struct {
	headers *[]kafka.Header
}

// NewHeaderCarrier creates a new HeaderCarrier wrapping the provided Kafka headers.
func NewHeaderCarrier(headers *[]kafka.Header) HeaderCarrier {
	return HeaderCarrier{headers: headers}
}

// Get returns the value of the first header matching key, or "" if absent.
func (c HeaderCarrier) Get(key string) string {
	for _, h := range *c.headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}

// Set replaces any existing header with key, otherwise appends a new one.
func (c HeaderCarrier) Set(key, value string) {
	for i := range *c.headers {
		if (*c.headers)[i].Key == key {
			(*c.headers)[i].Value = []byte(value)
			return
		}
	}
	*c.headers = append(*c.headers, kafka.Header{Key: key, Value: []byte(value)})
}

// Keys lists the header keys carried by this carrier.
func (c HeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(*c.headers))
	for _, h := range *c.headers {
		keys = append(keys, h.Key)
	}
	return keys
}
