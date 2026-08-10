// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package v3

import (
	"io"
	"time"

	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otelc/instrumentation/github.com/openai/openai-go/internal/streaming"
)

// newStreamingReader adapts the local operationType into the shared
// streaming package's OperationType and delegates the actual reader
// construction there. The streaming reader logic itself has no dependency on
// the versioned openai-go SDK, so v1/v2/v3 all share one implementation; see
// https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/issues/958.
func newStreamingReader(
	body io.ReadCloser,
	span trace.Span,
	start time.Time,
	op operationType,
) io.ReadCloser {
	var streamingOp streaming.OperationType
	switch op {
	case opChat:
		streamingOp = streaming.OpChat
	case opCompletion:
		streamingOp = streaming.OpCompletion
	}
	return streaming.NewStreamingReader(body, span, start, streamingOp)
}
