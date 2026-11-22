// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package propagation

import (
	nats "github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel/propagation"
)

var _ propagation.TextMapCarrier = HeaderCarrier{}

// HeaderCarrier adapts nats.Header to propagation.TextMapCarrier.
type HeaderCarrier nats.Header

// NewHeaderCarrier wraps a nats.Header as a TextMapCarrier.
func NewHeaderCarrier(h nats.Header) HeaderCarrier {
	return HeaderCarrier(h)
}

// Get returns the first value associated with key, or "".
func (c HeaderCarrier) Get(key string) string {
	if c == nil {
		return ""
	}
	return nats.Header(c).Get(key)
}

// Set sets the header entry associated with key to the single element value.
func (c HeaderCarrier) Set(key, value string) {
	if c == nil {
		return
	}
	nats.Header(c).Set(key, value)
}

// Keys returns all keys in the header.
func (c HeaderCarrier) Keys() []string {
	if c == nil {
		return nil
	}
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}
