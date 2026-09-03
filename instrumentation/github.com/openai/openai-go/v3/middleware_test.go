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

	semconv "go.opentelemetry.io/otelc/instrumentation/github.com/openai/openai-go/v3/semconv"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
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

// TestOtelMiddleware_RecordsDurationOnStreaming verifies that for streaming
// responses, gen_ai.client.operation.duration is not recorded when headers
// return, but is recorded once the response body stream is fully consumed.
func TestOtelMiddleware_RecordsDurationOnStreaming(t *testing.T) {
	reader := setupTestMeter(t)

	middleware := OtelMiddleware()

	req, _ := http.NewRequest(
		http.MethodPost,
		"http://api.openai.com/v1/chat/completions",
		io.NopCloser(bytes.NewReader([]byte(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`))),
	)
	streamData := "data: {\"id\":\"chatcmpl-1\",\"model\":\"gpt-4\",\"choices\":[{\"delta\":{\"content\":\"hello\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":5,\"total_tokens\":10}}\n\ndata: [DONE]\n\n"
	next := func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(streamData)),
		}, nil
	}

	resp, err := middleware(req, next)
	require.NoError(t, err)

	// Before reading the stream body, duration should not yet be recorded.
	dps := durationDataPoints(t, reader)
	require.Empty(t, dps, "duration must not be recorded before the stream is consumed")

	// Read and drain the stream body to trigger stream completion.
	_, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	_ = resp.Body.Close()

	// Duration should now be recorded exactly once.
	dps = durationDataPoints(t, reader)
	require.Len(t, dps, 1, "duration must be recorded once the stream finishes")
	assert.Equal(t, uint64(1), dps[0].Count)
	_, ok := dps[0].Attributes.Value(attribute.Key("error.type"))
	assert.False(t, ok, "error.type must not be present on successful stream")
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

// TestParseCompletionRequest_Penalties covers frequency_penalty and
// presence_penalty on the legacy completions endpoint.
func TestParseCompletionRequest_Penalties(t *testing.T) {
	body := []byte(`{
		"model":"gpt-3.5-turbo-instruct",
		"prompt":"hello",
		"frequency_penalty":0.5,
		"presence_penalty":-0.25
	}`)

	model, attrs, _ := parseCompletionRequest(body, false)
	assert.Equal(t, "gpt-3.5-turbo-instruct", model)

	got := map[attribute.Key]attribute.Value{}
	for _, a := range attrs {
		got[a.Key] = a.Value
	}

	require.Contains(t, got, semconv.GenAIRequestFrequencyPenaltyKey)
	assert.InDelta(t, 0.5, got[semconv.GenAIRequestFrequencyPenaltyKey].AsFloat64(), 1e-9)

	require.Contains(t, got, semconv.GenAIRequestPresencePenaltyKey)
	assert.InDelta(t, -0.25, got[semconv.GenAIRequestPresencePenaltyKey].AsFloat64(), 1e-9)
}

// Absent penalties must stay absent rather than being reported as zero, since
// 0 is a meaningful value for both parameters.
func TestParseCompletionRequest_PenaltiesOmitted(t *testing.T) {
	body := []byte(`{"model":"gpt-3.5-turbo-instruct","prompt":"hello"}`)

	_, attrs, _ := parseCompletionRequest(body, false)
	for _, a := range attrs {
		assert.NotEqual(t, semconv.GenAIRequestFrequencyPenaltyKey, a.Key)
		assert.NotEqual(t, semconv.GenAIRequestPresencePenaltyKey, a.Key)
	}
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

func TestParseCompletionRequest_Invalid(t *testing.T) {
	body := []byte(`invalid json`)
	model, attrs, _ := parseCompletionRequest(body, false)
	assert.Equal(t, "", model)
	assert.Nil(t, attrs)
}

func TestParseEmbeddingRequest_Invalid(t *testing.T) {
	body := []byte(`invalid json`)
	model, _ := parseEmbeddingRequest(body)
	assert.Equal(t, "", model)
}

func TestParseChatResponse_Invalid(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	_, span := tp.Tracer("test").Start(t.Context(), "test")
	body := []byte(`invalid json`)
	parseChatResponse(body, span, false)
}

func TestParseCompletionResponse_Invalid(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	_, span := tp.Tracer("test").Start(t.Context(), "test")
	body := []byte(`invalid json`)
	parseCompletionResponse(body, span, false)
}

func TestParseEmbeddingResponse_Invalid(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	_, span := tp.Tracer("test").Start(t.Context(), "test")
	body := []byte(`invalid json`)
	parseEmbeddingResponse(body, span)
}

func TestOtelMiddleware_ContentCapture_Enabled(t *testing.T) {
	reqBody := `{"messages":[{"role":"user","content":"hello"}]}`
	req, err := http.NewRequestWithContext(context.Background(), "POST", "https://api.openai.com/v1/chat/completions", bytes.NewBufferString(reqBody))
	require.NoError(t, err)

	resp := &http.Response{
		StatusCode: 200, Header: make(http.Header),
		Body: io.NopCloser(bytes.NewBufferString(`{}`)),
	}

	next := func(req *http.Request) (*http.Response, error) {
		return resp, nil
	}

	middleware := otelMiddleware(func() bool { return true })
	_, _ = middleware(req, next)
}

func TestOtelMiddleware_ErrorResponse(t *testing.T) {
	req, err := http.NewRequestWithContext(context.Background(), "POST", "https://api.openai.com/v1/chat/completions", bytes.NewBufferString(`{}`))
	require.NoError(t, err)

	resp := &http.Response{
		StatusCode: 400,
		Status:     "400 Bad Request",
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewBufferString(`{}`)),
	}

	next := func(req *http.Request) (*http.Response, error) {
		return resp, nil
	}

	middleware := OtelMiddleware()
	_, _ = middleware(req, next)
}

func TestParseChatResponse_ContentExtraction(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	_, span := tp.Tracer("test").Start(t.Context(), "test-json-extract")

	body := []byte(`{
        "id": "chatcmpl-123",
        "model": "gpt-4",
        "choices": [{
            "finish_reason": "stop",
            "message": {
                "content": "This is extracted content",
                "role": "assistant"
            }
        }]
    }`)
	parseChatResponse(body, span, true)

	spans := sr.Ended()
	if len(spans) > 0 {
		for _, event := range spans[0].Events() {
			if event.Name == "gen_ai.content.completion" {
				for _, attr := range event.Attributes {
					if attr.Key == "gen_ai.completion" {
						assert.Equal(t, "This is extracted content", attr.Value.AsString())
					}
				}
			}
		}
	}
}
