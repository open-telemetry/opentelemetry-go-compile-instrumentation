// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package shared

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func setupTestTracer(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return sr
}

// resetInit clears the sync.Once so each test gets a freshly-initialized
// tracer pulled from the globally-set TracerProvider.
func resetInit() {
	initOnce = sync.Once{}
}

// newJSONRequest builds a POST request with a JSON body targeting the given
// path. Path is stored on req.URL.Path so parseRoute can match it.
func newJSONRequest(t *testing.T, path, body string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		"https://api.openai.com"+path, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	return req
}

// attrMap flattens span attributes into a map for easy assertions.
func attrMap(span sdktrace.ReadOnlySpan) map[string]interface{} {
	m := make(map[string]interface{})
	for _, kv := range span.Attributes() {
		m[string(kv.Key)] = kv.Value.AsInterface()
	}
	return m
}

func TestOtelMiddleware_ChatCompletion_Success(t *testing.T) {
	resetInit()
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "OPENAI")
	sr := setupTestTracer(t)

	reqBody := `{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`
	req := newJSONRequest(t, "/v1/chat/completions", reqBody)

	respBody := `{"id":"chatcmpl-x","model":"gpt-4-0613","choices":[{"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":20}}`
	var seenReqBody string
	next := func(r *http.Request) (*http.Response, error) {
		buf, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		seenReqBody = string(buf)
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(respBody)),
		}, nil
	}

	resp, err := OtelMiddleware(req, next)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Next() must receive the original request body bytes unchanged.
	assert.Equal(t, reqBody, seenReqBody)

	// Caller must still be able to read the response body end-to-end.
	gotResp, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, respBody, string(gotResp))

	spans := sr.Ended()
	require.Len(t, spans, 1)
	span := spans[0]

	assert.Equal(t, "chat gpt-4", span.Name())
	assert.Equal(t, codes.Unset, span.Status().Code)

	attrs := attrMap(span)
	assert.Equal(t, "openai", attrs["gen_ai.system"])
	assert.Equal(t, "chat", attrs["gen_ai.operation.name"])
	assert.Equal(t, "gpt-4", attrs["gen_ai.request.model"])
	assert.Equal(t, "chatcmpl-x", attrs["gen_ai.response.id"])
	assert.Equal(t, "gpt-4-0613", attrs["gen_ai.response.model"])
	assert.Equal(t, int64(10), attrs["gen_ai.usage.input_tokens"])
	assert.Equal(t, int64(20), attrs["gen_ai.usage.output_tokens"])
}

func TestOtelMiddleware_TransportError(t *testing.T) {
	resetInit()
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "OPENAI")
	sr := setupTestTracer(t)

	req := newJSONRequest(t, "/v1/chat/completions", `{"model":"gpt-4"}`)
	wantErr := errors.New("connection refused")
	next := func(r *http.Request) (*http.Response, error) { return nil, wantErr }

	_, err := OtelMiddleware(req, next)
	require.ErrorIs(t, err, wantErr)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status().Code)
}

func TestOtelMiddleware_HTTP500(t *testing.T) {
	resetInit()
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "OPENAI")
	sr := setupTestTracer(t)

	req := newJSONRequest(t, "/v1/chat/completions", `{"model":"gpt-4"}`)
	next := func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Status:     "500 Internal Server Error",
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":"boom"}`)),
		}, nil
	}

	resp, err := OtelMiddleware(req, next)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status().Code)
}

func TestOtelMiddleware_Streaming(t *testing.T) {
	resetInit()
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "OPENAI")
	sr := setupTestTracer(t)

	// stream=true in request body → middleware must skip response buffering.
	req := newJSONRequest(t, "/v1/chat/completions", `{"model":"gpt-4","stream":true}`)

	// Simulate an SSE response. If the middleware buffers it, reading below
	// will see EOF and the assertion fails.
	sseBody := "data: {\"id\":\"1\"}\n\ndata: [DONE]\n\n"
	next := func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(sseBody)),
		}, nil
	}

	resp, err := OtelMiddleware(req, next)
	require.NoError(t, err)

	// Caller must still be able to read the full stream body.
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, sseBody, string(got))

	spans := sr.Ended()
	require.Len(t, spans, 1)
	span := spans[0]
	assert.Equal(t, "chat gpt-4", span.Name())
	// No response attributes parsed for streams.
	attrs := attrMap(span)
	_, hasID := attrs["gen_ai.response.id"]
	assert.False(t, hasID, "response.id must not be set for streaming responses")
}

func TestOtelMiddleware_NonJSONBody(t *testing.T) {
	resetInit()
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "OPENAI")
	sr := setupTestTracer(t)

	// Binary multipart-like body (image upload). Middleware should not
	// crash and should still create a span with empty model.
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		"https://api.openai.com/v1/images/generations",
		strings.NewReader("\x89PNG\r\n\x1a\nbinary-bytes"))
	require.NoError(t, err)

	next := func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader(`{"data":[]}`)),
		}, nil
	}

	_, err = OtelMiddleware(req, next)
	require.NoError(t, err)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	span := spans[0]
	// Route is unknown since path suffix doesn't match.
	assert.Equal(t, "unknown", span.Name())
	attrs := attrMap(span)
	assert.Equal(t, "unknown", attrs["gen_ai.operation.name"])
	assert.Equal(t, "", attrs["gen_ai.request.model"])
}

func TestOtelMiddleware_Disabled(t *testing.T) {
	resetInit()
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "OPENAI")
	sr := setupTestTracer(t)

	req := newJSONRequest(t, "/v1/chat/completions", `{"model":"gpt-4"}`)
	called := false
	next := func(r *http.Request) (*http.Response, error) {
		called = true
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("{}")),
		}, nil
	}

	_, err := OtelMiddleware(req, next)
	require.NoError(t, err)
	assert.True(t, called, "next must still be called when instrumentation is disabled")
	assert.Empty(t, sr.Ended(), "no spans when disabled")
}

func TestOtelMiddleware_AzurePath(t *testing.T) {
	resetInit()
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "OPENAI")
	sr := setupTestTracer(t)

	// Azure uses /openai/deployments/{name}/chat/completions — suffix match.
	req := newJSONRequest(t,
		"/openai/deployments/my-deployment/chat/completions",
		`{"model":"gpt-4","messages":[]}`)
	next := func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"x","model":"gpt-4","choices":[],"usage":{}}`)),
		}, nil
	}

	_, err := OtelMiddleware(req, next)
	require.NoError(t, err)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, "chat", attrMap(spans[0])["gen_ai.operation.name"])
}
