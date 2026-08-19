// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package v2

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestClassifyOperation(t *testing.T) {
	tests := []struct {
		path     string
		expected operationType
	}{
		{"/v1/chat/completions", opChat},
		{"/openai/deployments/gpt-4/chat/completions", opChat},
		{"/v1/completions", opCompletion},
		{"/v1/embeddings", opEmbedding},
		{"/v1/models", opUnknown},
		{"/v1/files", opUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.expected, classifyOperation(tt.path))
		})
	}
}

func TestGetProviderName(t *testing.T) {
	tests := []struct {
		host     string
		expected string
	}{
		{"api.openai.com", "openai"},
		{"myendpoint.azure.com", "azure"},
		{"api.deepseek.com", "deepseek"},
		{"dashscope.aliyuncs.com", "qwen"},
		{"api.groq.com", "groq"},
		{"localhost:11434", "local"},
		{"127.0.0.1:8080", "local"},
		{"custom-api.example.com", "openai"},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			assert.Equal(t, tt.expected, getProviderName(tt.host))
		})
	}
}

// TestGetProviderName_AmbiguousHostIsDeterministic guards against a
// regression to a map-based provider table: when a host matches more than
// one keyword, the result must always be the earliest match in declaration
// order, not whichever keyword a randomized map iteration happens to hit
// first.
func TestGetProviderName_AmbiguousHostIsDeterministic(t *testing.T) {
	host := "litellm-gateway.mistral-together-proxy.internal"

	for range 50 {
		assert.Equal(t, "together", getProviderName(host))
	}
}

func TestOperationName(t *testing.T) {
	assert.Equal(t, "chat", operationName(opChat))
	assert.Equal(t, "text_completion", operationName(opCompletion))
	assert.Equal(t, "embeddings", operationName(opEmbedding))
	assert.Equal(t, "", operationName(opUnknown))
}

func TestParseChatRequest(t *testing.T) {
	body := []byte(`{"model":"gpt-4","max_tokens":100,"temperature":0.7}`)
	model, attrs, _ := parseChatRequest(body, false)
	assert.Equal(t, "gpt-4", model)
	assert.NotEmpty(t, attrs)
}

func TestParseChatRequest_ContentCapture(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"hello"}]}`)
	_, _, prompts := parseChatRequest(body, true)
	assert.Equal(t, []string{"hello"}, prompts)
}

func TestParseChatRequest_MultimodalContentCapture(t *testing.T) {
	body := []byte(`{"model":"gpt-4-vision-preview","messages":[{"role":"user","content":[{"type":"text","text":"describe this"},{"type":"image_url","image_url":{"url":"https://example.com/image.png"}}]}]}`)
	model, _, prompts := parseChatRequest(body, true)

	assert.Equal(t, "gpt-4-vision-preview", model)
	assert.Equal(t, []string{`[{"type":"text","text":"describe this"},{"type":"image_url","image_url":{"url":"https://example.com/image.png"}}]`}, prompts)
}

func TestParseChatRequest_Invalid(t *testing.T) {
	body := []byte(`invalid json`)
	model, attrs, _ := parseChatRequest(body, false)
	assert.Equal(t, "", model)
	assert.Nil(t, attrs)
}

func TestParseCompletionRequest(t *testing.T) {
	body := []byte(`{"model":"gpt-3.5-turbo-instruct","max_tokens":50,"top_p":0.9}`)
	model, attrs, _ := parseCompletionRequest(body, false)
	assert.Equal(t, "gpt-3.5-turbo-instruct", model)
	assert.NotEmpty(t, attrs)
}

func TestParseCompletionRequest_ContentCapture(t *testing.T) {
	tests := []struct {
		name     string
		body     []byte
		expected []string
	}{
		{"string", []byte(`{"model":"gpt-3.5-turbo-instruct","prompt":"hello"}`), []string{"hello"}},
		{"array", []byte(`{"model":"gpt-3.5-turbo-instruct","prompt":["first","second"]}`), []string{"first", "second"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model, _, prompts := parseCompletionRequest(tt.body, true)
			assert.Equal(t, "gpt-3.5-turbo-instruct", model)
			assert.Equal(t, tt.expected, prompts)
		})
	}
}

func TestParseEmbeddingRequest(t *testing.T) {
	body := []byte(`{"model":"text-embedding-ada-002","input":"hello"}`)
	model, _ := parseEmbeddingRequest(body)
	assert.Equal(t, "text-embedding-ada-002", model)
}

func TestParseChatResponse_Valid(t *testing.T) {
	// This is a smoke test - full integration test would need OTel SDK setup
	body := []byte(`{
		"id":"chatcmpl-123",
		"model":"gpt-4",
		"choices":[{"finish_reason":"stop"}],
		"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}
	}`)

	var resp struct {
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

	err := json.Unmarshal(body, &resp)
	assert.NoError(t, err)
	assert.Equal(t, "chatcmpl-123", resp.ID)
	assert.Equal(t, int64(10), resp.Usage.PromptTokens)
	assert.Equal(t, int64(20), resp.Usage.CompletionTokens)
	assert.Equal(t, "stop", resp.Choices[0].FinishReason)
}

func TestParseChatResponse_ContentCapture(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tr := tp.Tracer("test")
	_, span := tr.Start(t.Context(), "test-content")

	body := []byte(`{
		"id":"chatcmpl-123",
		"model":"gpt-4",
		"choices":[{"message":{"content":"hello world"},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}
	}`)
	parseChatResponse(body, span, true)
	span.End()

	spans := sr.Ended()
	require.Len(t, spans, 1)

	hasCompletion := false
	for _, event := range spans[0].Events() {
		if event.Name == "gen_ai.content.completion" {
			hasCompletion = true
			require.Len(t, event.Attributes, 1)
			assert.Equal(t, "gen_ai.completion", string(event.Attributes[0].Key))
			assert.Equal(t, "hello world", event.Attributes[0].Value.AsString())
		}
	}
	assert.True(t, hasCompletion, "missing completion event")
}

func TestParseChatResponse_ContentCapture_Disabled(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tr := tp.Tracer("test")
	_, span := tr.Start(t.Context(), "test-content")

	body := []byte(`{
		"id":"chatcmpl-123",
		"model":"gpt-4",
		"choices":[{"message":{"content":"hello world"},"finish_reason":"stop"}]
	}`)
	parseChatResponse(body, span, false)
	span.End()

	spans := sr.Ended()
	require.Len(t, spans, 1)

	for _, event := range spans[0].Events() {
		if event.Name == "gen_ai.content.completion" {
			t.Errorf("expected no completion event, but got one")
		}
	}
}

func TestParseCompletionResponse_ContentCapture(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	_, span := tp.Tracer("test").Start(t.Context(), "completion-content")

	parseCompletionResponse([]byte(`{"choices":[{"text":"first","finish_reason":"stop"},{"text":"second","finish_reason":"stop"}]}`), span, true)
	span.End()

	spans := sr.Ended()
	require.Len(t, spans, 1)
	require.Len(t, spans[0].Events(), 2)
	assert.Equal(t, "first", spans[0].Events()[0].Attributes[0].Value.AsString())
	assert.Equal(t, "second", spans[0].Events()[1].Attributes[0].Value.AsString())
}

func TestContentEventsTruncateValues(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	_, span := tp.Tracer("test").Start(t.Context(), "truncated-content")

	recordContentEvents(span, "gen_ai.content.prompt", "gen_ai.prompt", []string{strings.Repeat("x", 16*1024+1)})
	span.End()

	spans := sr.Ended()
	require.Len(t, spans, 1)
	require.Len(t, spans[0].Events(), 1)
	captured := spans[0].Events()[0].Attributes[0].Value.AsString()
	assert.Len(t, captured, 16*1024)
	assert.True(t, strings.HasSuffix(captured, "... [truncated]"))
}
