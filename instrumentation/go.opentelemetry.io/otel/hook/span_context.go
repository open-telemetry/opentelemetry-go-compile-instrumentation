// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hook

import (
	_ "unsafe"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/hook"
	"go.opentelemetry.io/otel/trace"
)

//go:linkname spanFromGLS go.opentelemetry.io/otel/sdk/trace.spanFromGLS
func spanFromGLS() trace.Span

func spanFromContextOnExit(ictx hook.HookContext, span trace.Span) {
	if !span.SpanContext().IsValid() {
		glsSpan := spanFromGLS()
		if glsSpan != nil {
			ictx.SetReturnVal(0, glsSpan)
		}
	}
}
