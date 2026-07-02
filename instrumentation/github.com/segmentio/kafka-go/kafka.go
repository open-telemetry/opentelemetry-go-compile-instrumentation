// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package kafka

import (
	kafka "github.com/segmentio/kafka-go"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/runtime"
)

const (
	instrumentationName = "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/" +
		"instrumentation/github.com/segmentio/kafka-go"
	instrumentationKey = "KAFKA"
)

// kafkaEnablerImpl controls whether the kafka-go instrumentation is enabled.
type kafkaEnablerImpl struct{}

func (kafkaEnablerImpl) Enable() bool {
	return runtime.Instrumented(instrumentationKey)
}

var kafkaEnabler = kafkaEnablerImpl{}

// headerCarrier adapts a slice of kafka.Header to the OpenTelemetry
// TextMapCarrier interface so trace context can be propagated through Kafka
// message headers.
type headerCarrier struct {
	headers *[]kafka.Header
}

// Get returns the value of the first header matching key, or "" if absent.
func (c headerCarrier) Get(key string) string {
	for _, h := range *c.headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}

// Set replaces any existing header with key, otherwise appends a new one.
func (c headerCarrier) Set(key, value string) {
	for i := range *c.headers {
		if (*c.headers)[i].Key == key {
			(*c.headers)[i].Value = []byte(value)
			return
		}
	}
	*c.headers = append(*c.headers, kafka.Header{Key: key, Value: []byte(value)})
}

// Keys lists the header keys carried by this carrier.
func (c headerCarrier) Keys() []string {
	keys := make([]string, 0, len(*c.headers))
	for _, h := range *c.headers {
		keys = append(keys, h.Key)
	}
	return keys
}
