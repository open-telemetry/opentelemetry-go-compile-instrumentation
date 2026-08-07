// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package v3 // Use package v3 for the v3 folder's file

import (
	"context"
	"io"
	"time"

	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otelc/instrumentation/github.com/openai/openai-go/internal/streaming"
)

func newStreamingReader(body io.ReadCloser, span trace.Span, start time.Time, model, opName, provider string, op operationType, ctx context.Context) io.ReadCloser {
	var streamingOp streaming.OperationType
	switch op {
	case opChat:
		streamingOp = streaming.OpChat
	case opCompletion:
		streamingOp = streaming.OpCompletion
	}
	return streaming.NewStreamingReader(body, span, start, model, opName, provider, streamingOp, ctx)
}