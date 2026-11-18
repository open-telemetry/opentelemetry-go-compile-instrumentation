// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package genai

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sync"

	"go.opentelemetry.io/otel/codes"
	otelsemconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otelc/pkg/runtime"
)

// maxStreamEventSize bounds a single server-sent event line. Gemini sends one
// GenerateContentResponse chunk per line, so a well-formed event stays far
// below this; the cap only keeps a misbehaving stream from growing the buffer
// without limit.
const maxStreamEventSize = maxResponseBodySize

// streamEventTruncatedWarnOnce logs the size-cap warning once per process
// rather than once per oversized event.
var streamEventTruncatedWarnOnce sync.Once //nolint:gochecknoglobals // process-wide by design

// streamReader wraps a streamGenerateContent response body. It passes every
// byte through to the SDK unchanged, parses the server-sent events as they go
// by, and ends the span once the stream is finished: on EOF, on a read error,
// or when the caller closes the body.
//
// Gemini has no end-of-stream marker. Usage arrives as running totals and the
// finish reason on the last event, so the span takes the latest usage report
// and treats a stream that is closed before any finish reason as aborted.
type streamReader struct {
	body io.ReadCloser
	span trace.Span

	mu       sync.Mutex
	line     []byte
	skipping bool // the current line went over maxStreamEventSize
	summary  responseSummary
	ended    bool
}

func newStreamReader(body io.ReadCloser, span trace.Span) *streamReader {
	return &streamReader{body: body, span: span}
}

func (r *streamReader) Read(p []byte) (int, error) {
	n, err := r.body.Read(p)

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ended {
		return n, err
	}

	r.consume(p[:n])
	if err != nil {
		// A clean EOF means the server finished the stream, so the last line
		// may simply lack its trailing newline. Any other error can leave a
		// cut-off event in the buffer, which is dropped rather than parsed.
		if errors.Is(err, io.EOF) {
			r.flush()
			r.end(nil, false)
		} else {
			r.end(err, false)
		}
	}
	return n, err
}

func (r *streamReader) Close() error {
	r.mu.Lock()
	if !r.ended {
		r.flush()
		// Closing before the final event (an early break out of the SDK's
		// iterator, or a cancelled context) means the caller never saw the
		// whole response.
		r.end(nil, len(r.summary.reasons) == 0)
	}
	r.mu.Unlock()

	return r.body.Close()
}

// consume splits data into lines, carrying an incomplete trailing line over to
// the next call.
func (r *streamReader) consume(data []byte) {
	for len(data) > 0 {
		before, after, found := bytes.Cut(data, []byte("\n"))
		r.appendLine(before)
		if !found {
			return
		}
		r.processLine()
		data = after
	}
}

func (r *streamReader) appendLine(part []byte) {
	if r.skipping || len(part) == 0 {
		return
	}
	if len(r.line)+len(part) > maxStreamEventSize {
		r.line = r.line[:0]
		r.skipping = true
		streamEventTruncatedWarnOnce.Do(func() {
			runtime.Logger().Warn("Gemini stream event exceeds the size cap; event skipped",
				"limit_bytes", maxStreamEventSize)
		})
		return
	}
	r.line = append(r.line, part...)
}

// processLine parses the buffered line as an SSE "data:" field. Other SSE
// fields and blank separator lines carry nothing the span needs.
func (r *streamReader) processLine() {
	defer func() {
		r.line = r.line[:0]
		r.skipping = false
	}()
	if r.skipping {
		return
	}

	payload, isData := bytes.CutPrefix(bytes.TrimSpace(r.line), []byte("data:"))
	if !isData {
		return
	}

	var chunk generateContentResponse
	if err := json.Unmarshal(bytes.TrimSpace(payload), &chunk); err != nil {
		return
	}
	r.summary.add(&chunk)
}

func (r *streamReader) flush() {
	if len(r.line) > 0 || r.skipping {
		r.processLine()
	}
}

func (r *streamReader) end(readErr error, aborted bool) {
	r.ended = true
	r.span.SetAttributes(r.summary.attrs()...)

	switch {
	case readErr != nil:
		r.span.SetStatus(codes.Error, readErr.Error())
		r.span.RecordError(readErr)
		r.span.SetAttributes(otelsemconv.ErrorType(readErr))
	case aborted:
		r.span.SetStatus(codes.Error, "stream closed before completion")
	}

	r.span.End()
}
