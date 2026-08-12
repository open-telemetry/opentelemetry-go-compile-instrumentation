// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package v3

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// setupTestMeter wires the package-level operationDuration histogram to a
// manual reader so tests can assert what was recorded.
func setupTestMeter(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
	setupTestTracer(t)
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	var err error
	operationDuration, err = mp.Meter("test").Float64Histogram(
		"gen_ai.client.operation.duration", metric.WithUnit("s"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = mp.Shutdown(context.Background())
		operationDuration = nil
	})
	return reader
}

// durationDataPoints returns all data points for gen_ai.client.operation.duration.
func durationDataPoints(t *testing.T, reader *sdkmetric.ManualReader) []metricdata.HistogramDataPoint[float64] {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "gen_ai.client.operation.duration" {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			require.True(t, ok, "gen_ai.client.operation.duration must be a float64 histogram")
			return hist.DataPoints
		}
	}
	return nil
}

// TestOtelMiddleware_RecordsDuration verifies a successful chat completion
// emits exactly one gen_ai.client.operation.duration measurement.
func TestOtelMiddleware_RecordsDuration(t *testing.T) {
	reader := setupTestMeter(t)

	middleware := OtelMiddleware()

	req, _ := http.NewRequest(
		http.MethodPost,
		"http://api.openai.com/v1/chat/completions",
		io.NopCloser(bytes.NewReader([]byte(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`))),
	)
	next := func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"id":"chatcmpl-1","model":"gpt-4","choices":[{"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":10,"total_tokens":15}}`,
			)),
		}, nil
	}

	_, err := middleware(req, next)
	require.NoError(t, err)

	dps := durationDataPoints(t, reader)
	require.Len(t, dps, 1, "duration should be recorded once on success")
	assert.Equal(t, uint64(1), dps[0].Count)
	_, ok := dps[0].Attributes.Value(attribute.Key("error.type"))
	assert.False(t, ok, "error.type must not be present on success")
}

// TestOtelMiddleware_RecordsDurationOnHTTPError verifies the duration is
// recorded and error.type (numeric status code) is set on HTTP >=400 responses.
func TestOtelMiddleware_RecordsDurationOnHTTPError(t *testing.T) {
	reader := setupTestMeter(t)

	middleware := OtelMiddleware()

	req, _ := http.NewRequest(
		http.MethodPost,
		"http://api.openai.com/v1/chat/completions",
		io.NopCloser(bytes.NewReader([]byte(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`))),
	)
	next := func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Status:     "429 Too Many Requests",
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	}

	_, err := middleware(req, next)
	require.NoError(t, err)

	dps := durationDataPoints(t, reader)
	require.Len(t, dps, 1, "duration should be recorded once on HTTP error")
	assert.Equal(t, uint64(1), dps[0].Count)
	val, ok := dps[0].Attributes.Value(attribute.Key("error.type"))
	require.True(t, ok, "error.type must be present on HTTP error")
	assert.Equal(t, "429", val.AsString())
}

// TestOtelMiddleware_RecordsDurationOnTransportError verifies the duration is
// recorded and error.type is set when next() returns an error.
func TestOtelMiddleware_RecordsDurationOnTransportError(t *testing.T) {
	reader := setupTestMeter(t)

	middleware := OtelMiddleware()

	req, _ := http.NewRequest(
		http.MethodPost,
		"http://api.openai.com/v1/chat/completions",
		io.NopCloser(bytes.NewReader([]byte(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`))),
	)
	wantErr := errors.New("connection refused")
	next := func(r *http.Request) (*http.Response, error) {
		return nil, wantErr
	}

	_, err := middleware(req, next)
	require.ErrorIs(t, err, wantErr)

	dps := durationDataPoints(t, reader)
	require.Len(t, dps, 1, "duration should be recorded once on transport error")
	assert.Equal(t, uint64(1), dps[0].Count)
	_, ok := dps[0].Attributes.Value(attribute.Key("error.type"))
	require.True(t, ok, "error.type must be present on transport error")
}

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
	model, attrs := parseChatRequest(body)
	assert.Equal(t, "gpt-4", model)
	assert.NotEmpty(t, attrs)
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
