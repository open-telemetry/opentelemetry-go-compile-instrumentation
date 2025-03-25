// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrumenter

import (
	"os"
	"testing"
)

func TestGetSpanSuppressionStrategyFromEnv(t *testing.T) {
	tests := map[string]SpanSuppressorStrategy{
		"none":      &NoneStrategy{},
		"span-kind": &SpanKindStrategy{},
		"":          &SemConvStrategy{},
		"unknown":   &SemConvStrategy{},
	}

	for value, expectedStrategy := range tests {
		os.Setenv("OTEL_INSTRUMENTATION_EXPERIMENTAL_SPAN_SUPPRESSION_STRATEGY", value)
		defer os.Unsetenv("OTEL_INSTRUMENTATION_EXPERIMENTAL_SPAN_SUPPRESSION_STRATEGY")

		actualStrategy := getSpanSuppressionStrategyFromEnv()

		if expectedStrategy != actualStrategy {
			panic("Expected strategy does not match actual strategy")
		}
	}
}
