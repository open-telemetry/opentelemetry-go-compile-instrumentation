// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hook

import (
	_ "unsafe"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
	"go.opentelemetry.io/otel/trace"
)

//go:linkname traceContextDelSpan go.opentelemetry.io/otel/sdk/trace.TraceContextDelSpan
func traceContextDelSpan(span trace.Span)

func nonRecordingSpanEndOnEnter(ictx inst.HookContext, span interface{}, options ...interface{}) {
	if span != nil {
		traceContextDelSpan(span.(trace.Span))
	}
}

func recordingSpanEndOnEnter(ictx inst.HookContext, span interface{}, options ...interface{}) {
	if span != nil {
		traceContextDelSpan(span.(trace.Span))
	}
}
