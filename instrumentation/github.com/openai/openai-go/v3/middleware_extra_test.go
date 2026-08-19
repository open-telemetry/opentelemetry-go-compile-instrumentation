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
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

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
