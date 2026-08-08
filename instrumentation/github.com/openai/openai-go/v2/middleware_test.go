// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package v2

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
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
	temp07 := 0.7
	tests := []struct {
		name      string
		body      string
		wantModel string
		wantMax   int64
		wantTemp  *float64
	}{
		{
			name:      "max_tokens",
			body:      `{"model":"gpt-4","max_tokens":100,"temperature":0.7}`,
			wantModel: "gpt-4",
			wantMax:   100,
			wantTemp:  &temp07,
		},
		{
			name:      "max_completion_tokens",
			body:      `{"model":"o1-mini","max_completion_tokens":50}`,
			wantModel: "o1-mini",
			wantMax:   50,
		},
		{
			name:      "prefer max_completion_tokens",
			body:      `{"model":"gpt-4","max_tokens":100,"max_completion_tokens":50}`,
			wantModel: "gpt-4",
			wantMax:   50,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model, attrs := parseChatRequest([]byte(tt.body))
			assert.Equal(t, tt.wantModel, model)

			var gotMax *int64
			var gotTemp *float64
			for _, a := range attrs {
				switch a.Key {
				case "gen_ai.request.max_tokens":
					v := a.Value.AsInt64()
					gotMax = &v
				case "gen_ai.request.temperature":
					v := a.Value.AsFloat64()
					gotTemp = &v
				}
			}
			if assert.NotNil(t, gotMax) {
				assert.Equal(t, tt.wantMax, *gotMax)
			}
			if tt.wantTemp != nil {
				if assert.NotNil(t, gotTemp) {
					assert.InDelta(t, *tt.wantTemp, *gotTemp, 1e-9)
				}
			} else {
				assert.Nil(t, gotTemp)
			}
		})
	}
}

func TestParseChatRequest_Invalid(t *testing.T) {
	body := []byte(`invalid json`)
	model, attrs := parseChatRequest(body)
	assert.Equal(t, "", model)
	assert.Nil(t, attrs)
}

func TestParseCompletionRequest(t *testing.T) {
	body := []byte(`{"model":"gpt-3.5-turbo-instruct","max_tokens":50}`)
	model, attrs := parseCompletionRequest(body)
	assert.Equal(t, "gpt-3.5-turbo-instruct", model)
	assert.NotEmpty(t, attrs)
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
