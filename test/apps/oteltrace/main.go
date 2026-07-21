// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a test application that verifies the
// go.opentelemetry.io/otel/trace instrumentation can be applied
// independently of the OpenTelemetry SDK instrumentation.
//
// The application imports only the trace API package and calls
// trace.SpanFromContext(context.Background()). The build should succeed
// even though the SDK trace instrumentation is not present, and
// SpanFromContext should simply return the original no-op span.
package main

import (
	"context"

	"go.opentelemetry.io/otel/trace"
)

func main() {
	_ = trace.SpanFromContext(context.Background())
}
