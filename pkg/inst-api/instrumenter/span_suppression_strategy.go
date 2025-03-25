// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrumenter

import (
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"os"
)

type SpanSuppressorStrategy interface {
	create(spanKeys []attribute.Key) SpanSuppressor
}

type SemConvStrategy struct{}

func (t *SemConvStrategy) create(spanKeys []attribute.Key) SpanSuppressor {
	if len(spanKeys) == 0 {
		return NewNoopSpanSuppressor()
	}
	return NewSpanKeySuppressor(spanKeys)
}

type NoneStrategy struct{}

func (n *NoneStrategy) create(spanKeys []attribute.Key) SpanSuppressor {
	return NewNoopSpanSuppressor()
}

type SpanKindStrategy struct{}

func (s *SpanKindStrategy) create(spanKeys []attribute.Key) SpanSuppressor {
	return NewSpanKindSuppressor()
}

type SpanKindSuppressor struct {
	delegates map[trace.SpanKind]SpanSuppressor
}

func getSpanSuppressionStrategyFromEnv() SpanSuppressorStrategy {
	suppressionStrategy := os.Getenv("OTEL_INSTRUMENTATION_EXPERIMENTAL_SPAN_SUPPRESSION_STRATEGY")
	switch suppressionStrategy {
	case "none":
		return &NoneStrategy{}
	case "span-kind":
		return &SpanKindStrategy{}
	default:
		return &SemConvStrategy{}
	}
}
