// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/openai/semconv"
)

type streamBody struct {
	body      io.ReadCloser
	ctx       context.Context
	span      trace.Span
	start     time.Time
	operation string
	model     string

	mu           sync.Mutex
	lineBuf      []byte
	eventData    []string
	id           string
	respModel    string
	finish       []string
	inputTokens  int64
	outputTokens int64
	ended        bool
}

type streamChunk struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
	} `json:"usage"`
}

func newStreamBody(body io.ReadCloser, ctx context.Context, span trace.Span, start time.Time, operation, model string) io.ReadCloser {
	return &streamBody{
		body:      body,
		ctx:       ctx,
		span:      span,
		start:     start,
		operation: operation,
		model:     model,
	}
}

func (b *streamBody) Read(p []byte) (int, error) {
	n, err := b.body.Read(p)
	if n > 0 {
		b.observe(p[:n])
	}
	if err != nil {
		if err != io.EOF {
			b.span.RecordError(err)
			b.span.SetStatus(codes.Error, err.Error())
		}
		b.end(err)
	}
	return n, err
}

func (b *streamBody) Close() error {
	err := b.body.Close()
	if err != nil {
		b.span.RecordError(err)
		b.span.SetStatus(codes.Error, err.Error())
	}
	b.end(err)
	return err
}

func (b *streamBody) observe(chunk []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.lineBuf = append(b.lineBuf, chunk...)
	for {
		line, rest, ok := bytes.Cut(b.lineBuf, []byte{'\n'})
		if !ok {
			return
		}
		b.lineBuf = rest
		b.observeLine(strings.TrimSuffix(string(line), "\r"))
	}
}

func (b *streamBody) observeLine(line string) {
	if line == "" {
		b.flushEvent()
		return
	}
	if strings.HasPrefix(line, ":") {
		return
	}
	if data, ok := strings.CutPrefix(line, "data:"); ok {
		b.eventData = append(b.eventData, strings.TrimPrefix(data, " "))
	}
}

func (b *streamBody) flushEvent() {
	if len(b.eventData) == 0 {
		return
	}
	data := strings.Join(b.eventData, "\n")
	b.eventData = b.eventData[:0]
	if data == "[DONE]" {
		return
	}

	var chunk streamChunk
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return
	}
	if chunk.ID != "" {
		b.id = chunk.ID
	}
	if chunk.Model != "" {
		b.respModel = chunk.Model
	}
	for _, choice := range chunk.Choices {
		if choice.FinishReason != "" {
			b.finish = append(b.finish, choice.FinishReason)
		}
	}
	if chunk.Usage.PromptTokens > 0 {
		b.inputTokens = chunk.Usage.PromptTokens
	}
	if chunk.Usage.CompletionTokens > 0 {
		b.outputTokens = chunk.Usage.CompletionTokens
	}
}

func (b *streamBody) end(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ended {
		return
	}
	b.ended = true
	b.flushEvent()

	if b.id != "" || b.respModel != "" || len(b.finish) > 0 || b.inputTokens > 0 || b.outputTokens > 0 {
		b.span.SetAttributes(semconv.ChatCompletionResponseTraceAttrs(
			b.id,
			b.respModel,
			b.finish,
			b.inputTokens,
			b.outputTokens,
		)...)
		recordTokenUsage(b.ctx, b.operation, b.model, b.inputTokens, b.outputTokens)
	}

	durationErr := err
	if durationErr == io.EOF {
		durationErr = nil
	}
	recordDuration(b.ctx, b.operation, b.model, time.Since(b.start).Seconds(), durationErr)
	b.span.End()
}
