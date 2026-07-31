// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package trace

import (
	_ "unsafe"

	"go.opentelemetry.io/otel/trace"
)

//go:linkname traceContextAddSpan go.opentelemetry.io/otel/sdk/trace.traceContextAddSpan
func traceContextAddSpan(span trace.Span)

//go:linkname traceContextDelSpan go.opentelemetry.io/otel/sdk/trace.traceContextDelSpan
func traceContextDelSpan(span trace.Span)

func addSpanToGLS(span any) {
	if span != nil {
		if s, ok := span.(trace.Span); ok {
			traceContextAddSpan(s)
		}
	}
}

func deleteSpanFromGLS(span any) {
	if span != nil {
		if s, ok := span.(trace.Span); ok {
			traceContextDelSpan(s)
		}
	}
}
