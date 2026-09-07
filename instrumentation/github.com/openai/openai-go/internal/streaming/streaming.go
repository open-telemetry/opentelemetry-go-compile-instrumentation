// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package streaming implements the OpenAI GenAI streaming response reader
// shared by the v1, v2, and v3 openai-go instrumentation packages. It has no
// dependency on the versioned openai-go SDK, so a single copy serves all
// three; each version's own package only adapts its local operationType into
// OperationType and delegates here.
package streaming

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// OperationType identifies which OpenAI API shape a stream's chunks follow.
type OperationType int

const (
	OpChat OperationType = iota
	OpCompletion
)

type StreamingReader struct {
	reader        io.ReadCloser
	teeReader     io.Reader
	logBuffer     *bytes.Buffer
	lineBuffer    *bytes.Buffer
	start         time.Time
	first         time.Time
	inputTokens   int64
	outputTokens  int64
	totalTokens   int64
	id            string
	responseModel string
	reasons       []string
	span          trace.Span
	op            OperationType
	onDone        func()
	done          atomic.Bool
	completed     atomic.Bool
}

// errStreamAborted is reported when a stream is torn down (e.g. client
// cancellation or a context deadline) before a finish reason or the [DONE]
// marker was received.
var errStreamAborted = errors.New("stream aborted")

func NewStreamingReader(
	body io.ReadCloser,
	span trace.Span,
	start time.Time,
	op OperationType,
	onDone ...func(),
) *StreamingReader {
	var cb func()
	if len(onDone) > 0 {
		cb = onDone[0]
	}
	return &StreamingReader{
		reader: body,
		start:  start,
		span:   span,
		op:     op,
		onDone: cb,
	}
}

func (r *StreamingReader) Read(p []byte) (n int, err error) {
	if r.teeReader == nil {
		r.logBuffer = &bytes.Buffer{}
		r.lineBuffer = &bytes.Buffer{}
		r.teeReader = io.TeeReader(r.reader, r.logBuffer)
	}

	n, err = r.teeReader.Read(p)

	if n > 0 {
		r.processSSELines()
	}

	if err != nil && r.done.CompareAndSwap(false, true) {
		// Only a clean EOF means lineBuffer holds a complete, unterminated
		// final line. On any other read error the buffered bytes may be a
		// truncated mid-chunk fragment, so leave them unparsed rather than
		// attaching stale attributes to a span that ended in failure.
		r.finalize(err == io.EOF, err)
	}

	return n, err
}

func (r *StreamingReader) Close() error {
	if r.done.CompareAndSwap(false, true) {
		// Flush the line buffer first: a caller that stops reading before
		// EOF must still get the final chunk's attributes, and the flush may
		// itself recover the finish reason or [DONE] marker. Then decide
		// whether the stream completed or was torn down prematurely.
		r.flushRemaining()
		if r.completed.Load() || len(r.reasons) > 0 {
			r.finalize(false, nil)
		} else {
			r.finalize(false, errStreamAborted)
		}
	}
	if r.reader != nil {
		return r.reader.Close()
	}
	return nil
}

func (r *StreamingReader) finalize(flush bool, err error) {
	if flush {
		r.flushRemaining()
	}

	// A stream is complete once a finish reason or the [DONE] marker was
	// received. It is still an error when the stream terminates with a
	// transport error (e.g. truncated chunked encoding / io.ErrUnexpectedEOF
	// after the last chunk but before [DONE]): the host application will
	// surface that error, so telemetry must record it too instead of
	// reporting a successful 200.
	completed := r.completed.Load() || len(r.reasons) > 0
	hardErr := err != nil && !errors.Is(err, io.EOF)
	if !completed || hardErr {
		if len(r.reasons) == 0 {
			r.reasons = []string{"error"}
		}
		r.span.SetStatus(codes.Error, "stream aborted")
		if hardErr {
			r.span.RecordError(err)
		}
	}
	r.span.SetAttributes(
		genAIResponseFinishReasonsKey.StringSlice(r.reasons),
		genAIUsageInputTokensKey.Int64(r.inputTokens),
		genAIUsageOutputTokensKey.Int64(r.outputTokens),
		genAIUsageTotalTokensKey.Int64(r.totalTokens),
	)
	if r.id != "" {
		r.span.SetAttributes(genAIResponseIDKey.String(r.id))
	}
	if r.responseModel != "" {
		r.span.SetAttributes(genAIResponseModelKey.String(r.responseModel))
	}
	if !r.first.IsZero() {
		firstTokenUs := r.first.Sub(r.start).Microseconds()
		r.span.SetAttributes(genAIResponseTimeToFirstTokenKey.Int64(firstTokenUs))
	}

	r.span.End()
	if r.onDone != nil {
		r.onDone()
	}
}

// flushRemaining parses whatever is left in lineBuffer as a final line. The
// underlying reader can report an error (typically io.EOF) right after the
// last data line, with no trailing newline to trigger processSSELines, so
// that last chunk would otherwise sit unparsed in lineBuffer forever.
func (r *StreamingReader) flushRemaining() {
	if r.lineBuffer == nil || r.lineBuffer.Len() == 0 {
		return
	}

	line := bytes.TrimSpace(r.lineBuffer.Bytes())
	r.lineBuffer.Reset()
	if len(line) == 0 {
		return
	}

	payload, done := parseSSELine(line)
	if done {
		r.completed.Store(true)
		return
	}
	if payload == nil {
		return
	}
	r.processChunk(payload)
}

func (r *StreamingReader) processSSELines() {
	if r.logBuffer == nil || r.logBuffer.Len() == 0 {
		return
	}

	data := r.logBuffer.Bytes()
	r.lineBuffer.Write(data)
	r.logBuffer.Reset()

	allData := r.lineBuffer.Bytes()
	lines := bytes.Split(allData, []byte("\n"))

	var incompleteLine []byte
	for i, line := range lines {
		if i == len(lines)-1 {
			if len(line) > 0 {
				incompleteLine = line
			}
			break
		}

		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		payload, done := parseSSELine(line)
		if done {
			r.completed.Store(true)
			continue
		}
		if payload != nil {
			r.processChunk(payload)
		}
	}

	r.lineBuffer.Reset()
	if len(incompleteLine) > 0 {
		r.lineBuffer.Write(incompleteLine)
	}
}

func parseSSELine(line []byte) ([]byte, bool) {
	if !bytes.HasPrefix(line, []byte("data: ")) {
		return nil, false
	}
	payload := bytes.TrimPrefix(line, []byte("data: "))
	if bytes.Equal(payload, []byte("[DONE]")) {
		return nil, true
	}
	return payload, false
}

func (r *StreamingReader) processChunk(payload []byte) {
	if r.first.IsZero() {
		r.first = time.Now()
	}

	switch r.op {
	case OpChat:
		r.processChatChunk(payload)
	case OpCompletion:
		r.processCompletionChunk(payload)
	}
}

func (r *StreamingReader) processChatChunk(payload []byte) {
	var chunk struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Delta        struct {
				Content string `json:"content"`
			} `json:"delta"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
			TotalTokens      int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(payload, &chunk); err != nil {
		return
	}

	if chunk.ID != "" {
		r.id = chunk.ID
	}
	if chunk.Model != "" {
		r.responseModel = chunk.Model
	}
	if chunk.Usage.PromptTokens > 0 {
		r.inputTokens = chunk.Usage.PromptTokens
	}
	if chunk.Usage.CompletionTokens > 0 {
		r.outputTokens = chunk.Usage.CompletionTokens
	}
	if chunk.Usage.TotalTokens > 0 {
		r.totalTokens = chunk.Usage.TotalTokens
	}
	for _, c := range chunk.Choices {
		if c.FinishReason != "" {
			r.reasons = append(r.reasons, c.FinishReason)
		}
	}
}

func (r *StreamingReader) processCompletionChunk(payload []byte) {
	var chunk struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
			TotalTokens      int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(payload, &chunk); err != nil {
		return
	}

	if chunk.ID != "" {
		r.id = chunk.ID
	}
	if chunk.Model != "" {
		r.responseModel = chunk.Model
	}
	if chunk.Usage.PromptTokens > 0 {
		r.inputTokens = chunk.Usage.PromptTokens
	}
	if chunk.Usage.CompletionTokens > 0 {
		r.outputTokens = chunk.Usage.CompletionTokens
	}
	if chunk.Usage.TotalTokens > 0 {
		r.totalTokens = chunk.Usage.TotalTokens
	}
	for _, c := range chunk.Choices {
		if c.FinishReason != "" {
			r.reasons = append(r.reasons, c.FinishReason)
		}
	}
}
