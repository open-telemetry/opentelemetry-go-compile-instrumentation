// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hook

import (
	_ "unsafe"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
	"go.opentelemetry.io/otel/trace"
)

//go:linkname traceContextDelSpan go.opentelemetry.io/otel/sdk/trace.traceContextDelSpan
func traceContextDelSpan(span trace.Span)

func nonRecordingSpanEndOnEnter(ictx inst.HookContext, span interface{}, options ...interface{}) {
	deleteFromGls(span)
}

func recordingSpanEndOnEnter(ictx inst.HookContext, span interface{}, options ...interface{}) {
	deleteFromGls(span)
}

func deleteFromGls(span interface{}) {
	if span != nil {
		if s, ok := span.(trace.Span); ok {
			traceContextDelSpan(s)
		}
	}
}
