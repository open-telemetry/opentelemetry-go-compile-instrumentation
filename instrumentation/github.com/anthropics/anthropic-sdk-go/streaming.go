// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package anthropic

import (
	"io"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/trace"
)

// streamingReader wraps a streaming (SSE) response body so the span stays open
// until the stream is consumed or closed.
//
// TODO(#679): accumulate Anthropic stream events. Unlike OpenAI's SSE format
// (bare "data:" lines terminated by "data: [DONE]"), the Messages API emits
// named events with no [DONE] sentinel:
//   - message_start   -> response id, model, usage.input_tokens
//   - message_delta   -> delta.stop_reason, cumulative usage.output_tokens
//   - message_stop    -> end of stream
//
// On finalize, set gen_ai.response.* and gen_ai.usage.* plus
// gen_ai.response.time_to_first_token, mirroring the openai-go streamingReader.
type streamingReader struct {
	reader io.ReadCloser
	span   trace.Span
	start  time.Time
	done   atomic.Bool
}

func newStreamingReader(body io.ReadCloser, span trace.Span, start time.Time) *streamingReader {
	return &streamingReader{
		reader: body,
		span:   span,
		start:  start,
	}
}

func (r *streamingReader) Read(p []byte) (n int, err error) {
	n, err = r.reader.Read(p)
	if err != nil && r.done.CompareAndSwap(false, true) {
		r.finalize()
	}
	return n, err
}

func (r *streamingReader) Close() error {
	if r.done.CompareAndSwap(false, true) {
		r.finalize()
	}
	if r.reader != nil {
		return r.reader.Close()
	}
	return nil
}

func (r *streamingReader) finalize() {
	// TODO(#679): set accumulated response attributes before ending the span.
	r.span.End()
}
