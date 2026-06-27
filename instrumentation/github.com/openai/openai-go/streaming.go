// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/github.com/openai/openai-go/semconv"
)

type streamingReader struct {
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
	model         string
	opName        string
	provider      string
	op            operationType
	done          atomic.Bool
}

func newStreamingReader(body io.ReadCloser, span trace.Span, start time.Time, model, opName, provider string, op operationType, _ context.Context) *streamingReader {
	return &streamingReader{
		reader:   body,
		start:    start,
		span:     span,
		model:    model,
		opName:   opName,
		provider: provider,
		op:       op,
	}
}

func (r *streamingReader) Read(p []byte) (n int, err error) {
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
	r.span.SetAttributes(
		semconv.GenAIResponseFinishReasons(r.reasons),
		semconv.GenAIUsageInputTokens(r.inputTokens),
		semconv.GenAIUsageOutputTokens(r.outputTokens),
		semconv.GenAIUsageTotalTokens(r.totalTokens),
	)
	if r.id != "" {
		r.span.SetAttributes(semconv.GenAIResponseID(r.id))
	}
	if r.responseModel != "" {
		r.span.SetAttributes(semconv.GenAIResponseModel(r.responseModel))
	}
	if !r.first.IsZero() {
		firstTokenUs := r.first.Sub(r.start).Microseconds()
		r.span.SetAttributes(semconv.GenAIResponseTimeToFirstToken(firstTokenUs))
	}

	r.span.End()
}

func (r *streamingReader) processSSELines() {
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

func (r *streamingReader) processChunk(payload []byte) {
	if r.first.IsZero() {
		r.first = time.Now()
	}

	switch r.op {
	case opChat:
		r.processChatChunk(payload)
	case opCompletion:
		r.processCompletionChunk(payload)
	}
}

func (r *streamingReader) processChatChunk(payload []byte) {
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

func (r *streamingReader) processCompletionChunk(payload []byte) {
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
