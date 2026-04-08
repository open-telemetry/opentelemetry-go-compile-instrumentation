// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package shared

import (
	_ "unsafe"
)

// GetTraceAndSpanId returns the current trace ID and span ID from GLS.
// This is useful for log instrumentation to inject trace context into log messages.
//
//go:linkname GetTraceAndSpanId go.opentelemetry.io/otel/sdk/trace.GetTraceAndSpanId
func GetTraceAndSpanId() (traceId string, spanId string)
