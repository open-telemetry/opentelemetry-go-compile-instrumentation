// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package trace

import (
	_ "unsafe"

	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otelc/pkg/hook"
	"go.opentelemetry.io/otelc/pkg/runtime"
)

func afterSpanFromContext(ictx hook.HookContext, span trace.Span) {
	if !span.SpanContext().IsValid() {
		glsSpan := runtime.GetSpanFromGLS()
		if glsSpan != nil {
			ictx.SetReturnVal(0, glsSpan)
		}
	}
}
