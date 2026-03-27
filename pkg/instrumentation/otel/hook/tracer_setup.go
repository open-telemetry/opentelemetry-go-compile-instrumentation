// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hook

import (
	_ "unsafe"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
	"go.opentelemetry.io/otel/trace"
)

//go:linkname traceContextAddSpan go.opentelemetry.io/otel/sdk/trace.traceContextAddSpan
func traceContextAddSpan(span trace.Span)

func newRecordingSpanOnExit(ictx inst.HookContext, span interface{}) {
	if span != nil {
		s, ok := span.(trace.Span)
		if ok {
			traceContextAddSpan(s)
		}
	}
}

func newNonRecordingSpanOnExit(ictx inst.HookContext, span interface{}) {
	if span != nil {
		if s, ok := span.(trace.Span); ok {
			traceContextAddSpan(s)
		}
	}
}
