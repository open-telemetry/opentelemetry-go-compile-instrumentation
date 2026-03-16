// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
package segmentio

import (
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/shared"
	kafka "github.com/segmentio/kafka-go"
)

const (
	instrumentationName = "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/kafka"
	instrumentationKey  = "KAFKA"
)

// kafkaClientEnabler controls whether client instrumentation is enabled
type kafkaClientEnabler struct{}

func (g kafkaClientEnabler) Enable() bool {
	return shared.Instrumented(instrumentationKey)
}

var kafkaEnabler = kafkaClientEnabler{}

type KafkaHeaderCarrier struct {
	headers *[]kafka.Header
}

func (c KafkaHeaderCarrier) Get(key string) string {
	for _, h := range *c.headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}

func (c KafkaHeaderCarrier) Set(key, value string) {
	*c.headers = append(*c.headers, kafka.Header{
		Key:   key,
		Value: []byte(value),
	})
}

func (c KafkaHeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(*c.headers))
	for _, h := range *c.headers {
		keys = append(keys, h.Key)
	}
	return keys
}
