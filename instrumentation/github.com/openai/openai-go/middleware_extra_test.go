// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package v1

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
	model, attrs := parseCompletionRequest(body)
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
	parseChatResponse(body, span)
}

func TestParseCompletionResponse_Invalid(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	_, span := tp.Tracer("test").Start(t.Context(), "test")
	body := []byte(`invalid json`)
	parseCompletionResponse(body, span)
}

func TestParseEmbeddingResponse_Invalid(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	_, span := tp.Tracer("test").Start(t.Context(), "test")
	body := []byte(`invalid json`)
	parseEmbeddingResponse(body, span)
}

func TestOtelMiddleware_ContentCapture_Enabled(t *testing.T) {
	orig := captureContentEnabled
	captureContentEnabled = func() bool { return true }
	defer func() { captureContentEnabled = orig }()

	reqBody := `{"messages":[{"role":"user","content":"hello"}]}`
	req, err := http.NewRequestWithContext(context.Background(), "POST", "https://api.openai.com/v1/chat/completions", bytes.NewBufferString(reqBody))
	require.NoError(t, err)

	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewBufferString(`{}`)),
	}

	next := func(req *http.Request) (*http.Response, error) {
		return resp, nil
	}

	middleware := OtelMiddleware()
	_, _ = middleware(req, next)
}

func TestOtelMiddleware_ErrorResponse(t *testing.T) {
	req, err := http.NewRequestWithContext(context.Background(), "POST", "https://api.openai.com/v1/chat/completions", bytes.NewBufferString(`{}`))
	require.NoError(t, err)

	resp := &http.Response{
		StatusCode: 400,
		Status:     "400 Bad Request",
		Body:       io.NopCloser(bytes.NewBufferString(`{}`)),
	}

	next := func(req *http.Request) (*http.Response, error) {
		return resp, nil
	}

	middleware := OtelMiddleware()
	_, _ = middleware(req, next)
}
