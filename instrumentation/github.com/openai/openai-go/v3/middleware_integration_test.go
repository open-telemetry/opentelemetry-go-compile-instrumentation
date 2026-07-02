// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package v3

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func setupTestTracer(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	tracer = tp.Tracer("test")
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return sr
}

func TestOtelMiddleware_ChatCompletion(t *testing.T) {
	sr := setupTestTracer(t)

	middleware := OtelMiddleware()

	reqBody := `{"model":"gpt-4","max_tokens":100,"temperature":0.7,"top_p":0.9,"frequency_penalty":0.5,"presence_penalty":0.3}`
	req, _ := http.NewRequest("POST", "http://api.openai.com/v1/chat/completions", io.NopCloser(bytes.NewReader([]byte(reqBody))))

	respBody := `{"id":"chatcmpl-123","model":"gpt-4","choices":[{"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`
	next := func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader([]byte(respBody))),
		}, nil
	}

	resp, err := middleware(req, next)
	require.NoError(t, err)
	require.NotNil(t, resp)

	spans := sr.Ended()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, "chat gpt-4", span.Name())

	attrs := span.Attributes()
	assertAttribute(t, attrs, "gen_ai.system", "openai")
	assertAttribute(t, attrs, "gen_ai.operation.name", "chat")
	assertAttribute(t, attrs, "gen_ai.request.model", "gpt-4")
	assertAttribute(t, attrs, "gen_ai.provider.name", "openai")
	assertAttribute(t, attrs, "gen_ai.response.id", "chatcmpl-123")
	assertAttribute(t, attrs, "gen_ai.response.model", "gpt-4")
	assertInt64Attribute(t, attrs, "gen_ai.usage.input_tokens", 10)
	assertInt64Attribute(t, attrs, "gen_ai.usage.output_tokens", 20)
	assertInt64Attribute(t, attrs, "gen_ai.usage.total_tokens", 30)
	assertInt64Attribute(t, attrs, "gen_ai.request.max_tokens", 100)
	assertFloat64Attribute(t, attrs, "gen_ai.request.temperature", 0.7)
	assertFloat64Attribute(t, attrs, "gen_ai.request.top_p", 0.9)
	assertFloat64Attribute(t, attrs, "gen_ai.request.frequency_penalty", 0.5)
	assertFloat64Attribute(t, attrs, "gen_ai.request.presence_penalty", 0.3)
}

func TestOtelMiddleware_Completion(t *testing.T) {
	sr := setupTestTracer(t)

	middleware := OtelMiddleware()

	reqBody := `{"model":"gpt-3.5-turbo-instruct","max_tokens":50,"temperature":0.5}`
	req, _ := http.NewRequest("POST", "http://api.openai.com/v1/completions", io.NopCloser(bytes.NewReader([]byte(reqBody))))

	respBody := `{"id":"cmpl-456","model":"gpt-3.5-turbo-instruct","choices":[{"finish_reason":"length"}],"usage":{"prompt_tokens":5,"completion_tokens":50,"total_tokens":55}}`
	next := func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader([]byte(respBody))),
		}, nil
	}

	resp, err := middleware(req, next)
	require.NoError(t, err)
	require.NotNil(t, resp)

	spans := sr.Ended()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, "text_completion gpt-3.5-turbo-instruct", span.Name())

	attrs := span.Attributes()
	assertAttribute(t, attrs, "gen_ai.system", "openai")
	assertAttribute(t, attrs, "gen_ai.operation.name", "text_completion")
	assertAttribute(t, attrs, "gen_ai.request.model", "gpt-3.5-turbo-instruct")
	assertAttribute(t, attrs, "gen_ai.response.id", "cmpl-456")
	assertInt64Attribute(t, attrs, "gen_ai.usage.input_tokens", 5)
	assertInt64Attribute(t, attrs, "gen_ai.usage.output_tokens", 50)
}

func TestOtelMiddleware_Embedding(t *testing.T) {
	sr := setupTestTracer(t)

	middleware := OtelMiddleware()

	reqBody := `{"model":"text-embedding-ada-002","input":"hello world"}`
	req, _ := http.NewRequest("POST", "http://api.openai.com/v1/embeddings", io.NopCloser(bytes.NewReader([]byte(reqBody))))

	respBody := `{"model":"text-embedding-ada-002","usage":{"prompt_tokens":2,"total_tokens":2}}`
	next := func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader([]byte(respBody))),
		}, nil
	}

	resp, err := middleware(req, next)
	require.NoError(t, err)
	require.NotNil(t, resp)

	spans := sr.Ended()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, "embeddings text-embedding-ada-002", span.Name())

	attrs := span.Attributes()
	assertAttribute(t, attrs, "gen_ai.system", "openai")
	assertAttribute(t, attrs, "gen_ai.operation.name", "embeddings")
	assertAttribute(t, attrs, "gen_ai.request.model", "text-embedding-ada-002")
	assertAttribute(t, attrs, "gen_ai.response.model", "text-embedding-ada-002")
	assertInt64Attribute(t, attrs, "gen_ai.usage.input_tokens", 2)
	assertInt64Attribute(t, attrs, "gen_ai.usage.total_tokens", 2)
}

func TestOtelMiddleware_UnknownOperation(t *testing.T) {
	sr := setupTestTracer(t)

	middleware := OtelMiddleware()

	reqBody := `{"model":"gpt-4"}`
	req, _ := http.NewRequest("POST", "http://api.openai.com/v1/models", io.NopCloser(bytes.NewReader([]byte(reqBody))))

	next := func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader([]byte(`{}`))),
		}, nil
	}

	resp, err := middleware(req, next)
	require.NoError(t, err)
	require.NotNil(t, resp)

	spans := sr.Ended()
	assert.Len(t, spans, 0, "unknown operations should not create spans")
}

func TestOtelMiddleware_NilBody(t *testing.T) {
	sr := setupTestTracer(t)

	middleware := OtelMiddleware()

	req, _ := http.NewRequest("GET", "http://api.openai.com/v1/chat/completions", nil)

	next := func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader([]byte(`{}`))),
		}, nil
	}

	resp, err := middleware(req, next)
	require.NoError(t, err)
	require.NotNil(t, resp)

	spans := sr.Ended()
	assert.Len(t, spans, 0, "nil body should skip instrumentation")
}

func TestOtelMiddleware_NextError(t *testing.T) {
	sr := setupTestTracer(t)

	middleware := OtelMiddleware()

	reqBody := `{"model":"gpt-4"}`
	req, _ := http.NewRequest("POST", "http://api.openai.com/v1/chat/completions", io.NopCloser(bytes.NewReader([]byte(reqBody))))

	next := func(r *http.Request) (*http.Response, error) {
		return nil, assert.AnError
	}

	resp, err := middleware(req, next)
	assert.Error(t, err)
	assert.Nil(t, resp)

	spans := sr.Ended()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, "chat gpt-4", span.Name())
}

func TestOtelMiddleware_ProviderDetection(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		expected string
	}{
		{"deepseek", "api.deepseek.com", "deepseek"},
		{"azure", "myendpoint.azure.com", "azure"},
		{"local", "localhost:11434", "local"},
		{"groq", "api.groq.com", "groq"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := setupTestTracer(t)
			middleware := OtelMiddleware()

			reqBody := `{"model":"test-model"}`
			req, _ := http.NewRequest("POST", "http://"+tt.host+"/v1/chat/completions", io.NopCloser(bytes.NewReader([]byte(reqBody))))

			respBody := `{"id":"test","model":"test-model","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
			next := func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: 200,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(bytes.NewReader([]byte(respBody))),
				}, nil
			}

			_, err := middleware(req, next)
			require.NoError(t, err)

			spans := sr.Ended()
			require.Len(t, spans, 1)
			assertAttribute(t, spans[0].Attributes(), "gen_ai.provider.name", tt.expected)
		})
	}
}

func TestOtelMiddleware_StreamingResponse(t *testing.T) {
	sr := setupTestTracer(t)

	middleware := OtelMiddleware()

	reqBody := `{"model":"gpt-4","stream":true}`
	req, _ := http.NewRequest("POST", "http://api.openai.com/v1/chat/completions", io.NopCloser(bytes.NewReader([]byte(reqBody))))

	streamData := "data: {\"id\":\"chatcmpl-stream\",\"model\":\"gpt-4\",\"choices\":[{\"delta\":{\"content\":\"Hello\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"chatcmpl-stream\",\"model\":\"gpt-4\",\"choices\":[{\"delta\":{\"content\":\" world\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":2,\"total_tokens\":7}}\n\ndata: [DONE]\n\n"
	next := func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(bytes.NewReader([]byte(streamData))),
		}, nil
	}

	resp, err := middleware(req, next)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Read the streaming body to completion to trigger finalization
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "Hello")
	resp.Body.Close()

	spans := sr.Ended()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, "chat gpt-4", span.Name())

	attrs := span.Attributes()
	assertAttribute(t, attrs, "gen_ai.system", "openai")
	assertAttribute(t, attrs, "gen_ai.request.is_stream", "true")
	assertAttribute(t, attrs, "gen_ai.response.id", "chatcmpl-stream")
	assertAttribute(t, attrs, "gen_ai.response.model", "gpt-4")
	assertInt64Attribute(t, attrs, "gen_ai.usage.input_tokens", 5)
	assertInt64Attribute(t, attrs, "gen_ai.usage.output_tokens", 2)
	assertInt64Attribute(t, attrs, "gen_ai.usage.total_tokens", 7)
}

func TestOtelMiddleware_AzurePath(t *testing.T) {
	sr := setupTestTracer(t)

	middleware := OtelMiddleware()

	reqBody := `{"model":"gpt-4"}`
	req, _ := http.NewRequest("POST", "http://myendpoint.azure.com/openai/deployments/gpt-4/chat/completions", io.NopCloser(bytes.NewReader([]byte(reqBody))))

	respBody := `{"id":"azure-123","model":"gpt-4","choices":[{"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`
	next := func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader([]byte(respBody))),
		}, nil
	}

	resp, err := middleware(req, next)
	require.NoError(t, err)
	require.NotNil(t, resp)

	spans := sr.Ended()
	require.Len(t, spans, 1)

	attrs := spans[0].Attributes()
	assertAttribute(t, attrs, "gen_ai.operation.name", "chat")
	assertAttribute(t, attrs, "gen_ai.provider.name", "azure")
}
