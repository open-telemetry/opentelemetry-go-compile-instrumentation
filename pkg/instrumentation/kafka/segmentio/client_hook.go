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
