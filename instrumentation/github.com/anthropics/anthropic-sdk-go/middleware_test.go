// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package anthropic

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestClassifyOperation(t *testing.T) {
	tests := []struct {
		path     string
		expected operationType
	}{
		{"/v1/messages", opMessages},
		{"/anthropic/v1/messages", opMessages},
		{"/v1/messages/count_tokens", opCountTokens},
		{"/anthropic/v1/messages/count_tokens", opCountTokens},
		{"/v1/messages/batches", opUnknown},
		{"/v1/models", opUnknown},
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
		{"api.anthropic.com", "anthropic"},
		{"localhost:8080", "local"},
		{"127.0.0.1:8080", "local"},
		{"custom-proxy.example.com", "anthropic"},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			assert.Equal(t, tt.expected, getProviderName(tt.host))
		})
	}
}

func TestOperationName(t *testing.T) {
	assert.Equal(t, "chat", operationName(opMessages))
	assert.Equal(t, "count_tokens", operationName(opCountTokens))
	assert.Equal(t, "", operationName(opUnknown))
}

func TestParseMessagesRequest(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-5","max_tokens":1024,"temperature":0.7,"top_p":0.9,"top_k":40}`)
	model, isStream, attrs := parseMessagesRequest(body)
	assert.Equal(t, "claude-sonnet-4-5", model)
	assert.False(t, isStream)
	assert.Len(t, attrs, 4)
}

func TestParseMessagesRequest_Stream(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-5","max_tokens":1024,"stream":true}`)
	model, isStream, attrs := parseMessagesRequest(body)
	assert.Equal(t, "claude-sonnet-4-5", model)
	assert.True(t, isStream)
	assert.Len(t, attrs, 1)
}

func TestParseMessagesRequest_Invalid(t *testing.T) {
	body := []byte(`invalid json`)
	model, isStream, attrs := parseMessagesRequest(body)
	assert.Equal(t, "", model)
	assert.False(t, isStream)
	assert.Nil(t, attrs)
}

func TestParseCountTokensResponse(t *testing.T) {
	sr := setupTestTracer(t)
	_, span := tracer.Start(context.Background(), "test")
	parseCountTokensResponse([]byte(`{"input_tokens":42}`), span)
	span.End()

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assertInt64Attribute(t, spans[0].Attributes(), "gen_ai.usage.input_tokens", 42)
}

func TestParseCountTokensResponse_Invalid(t *testing.T) {
	sr := setupTestTracer(t)
	_, span := tracer.Start(context.Background(), "test")
	parseCountTokensResponse([]byte(`not json`), span)
	span.End()

	spans := sr.Ended()
	require.Len(t, spans, 1)
	_, found := findAttribute(spans[0].Attributes(), "gen_ai.usage.input_tokens")
	assert.False(t, found, "malformed JSON should not set any attribute")
}

func setupTestTracer(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	tracer = tp.Tracer("test")
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return sr
}

// setupTestMeter wires the package-level operationDuration histogram to a
// manual reader so tests can assert what was recorded.
func setupTestMeter(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
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

// durationCount collects the recorded gen_ai.client.operation.duration
// histogram and returns the total number of measurements across data points.
func durationCount(t *testing.T, reader *sdkmetric.ManualReader) uint64 {
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
			var count uint64
			for _, dp := range hist.DataPoints {
				count += dp.Count
			}
			return count
		}
	}
	return 0
}

// TestOtelMiddleware_RecordsDuration verifies a successful Messages call emits
// exactly one gen_ai.client.operation.duration measurement.
func TestOtelMiddleware_RecordsDuration(t *testing.T) {
	setupTestTracer(t)
	reader := setupTestMeter(t)

	middleware := OtelMiddleware()

	req, _ := http.NewRequest(
		http.MethodPost,
		"http://api.anthropic.com/v1/messages",
		io.NopCloser(bytes.NewReader([]byte(`{"model":"claude-sonnet-4-5","max_tokens":10}`))),
	)
	next := func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"id":"msg_1","model":"claude-sonnet-4-5","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":2}}`,
			)),
		}, nil
	}

	_, err := middleware(req, next)
	require.NoError(t, err)

	assert.Equal(t, uint64(1), durationCount(t, reader), "duration should be recorded once on success")
}

// TestOtelMiddleware_RecordsDurationOnError verifies the duration is recorded
// even when the call fails, so error latency is observable too.
func TestOtelMiddleware_RecordsDurationOnError(t *testing.T) {
	setupTestTracer(t)
	reader := setupTestMeter(t)

	middleware := OtelMiddleware()

	req, _ := http.NewRequest(
		http.MethodPost,
		"http://api.anthropic.com/v1/messages",
		io.NopCloser(bytes.NewReader([]byte(`{"model":"claude-sonnet-4-5","max_tokens":10}`))),
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

	assert.Equal(t, uint64(1), durationCount(t, reader), "duration should be recorded once on HTTP error")
}

// TestOtelMiddleware_Messages defines the expected span shape for a
// non-streaming Messages API call (POST /v1/messages).
func TestOtelMiddleware_Messages(t *testing.T) {
	sr := setupTestTracer(t)

	middleware := OtelMiddleware()

	reqBody := `{"model":"claude-sonnet-4-5","max_tokens":1024,"temperature":0.7,"top_p":0.9,"top_k":40,"messages":[{"role":"user","content":"Hello"}]}`
	req, _ := http.NewRequest(
		"POST",
		"http://api.anthropic.com/v1/messages",
		io.NopCloser(bytes.NewReader([]byte(reqBody))),
	)

	respBody := `{"id":"msg_test_123","type":"message","role":"assistant","model":"claude-sonnet-4-5","stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":20,"cache_read_input_tokens":7,"cache_creation_input_tokens":3}}`
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
	assert.Equal(t, "chat claude-sonnet-4-5", span.Name())

	attrs := span.Attributes()
	assertAttribute(t, attrs, "gen_ai.system", "anthropic")
	assertAttribute(t, attrs, "gen_ai.operation.name", "chat")
	assertAttribute(t, attrs, "gen_ai.request.model", "claude-sonnet-4-5")
	assertAttribute(t, attrs, "gen_ai.provider.name", "anthropic")
	assertInt64Attribute(t, attrs, "gen_ai.request.max_tokens", 1024)
	assertFloat64Attribute(t, attrs, "gen_ai.request.temperature", 0.7)
	assertFloat64Attribute(t, attrs, "gen_ai.request.top_p", 0.9)
	assertInt64Attribute(t, attrs, "gen_ai.request.top_k", 40)
	assertAttribute(t, attrs, "gen_ai.response.id", "msg_test_123")
	assertAttribute(t, attrs, "gen_ai.response.model", "claude-sonnet-4-5")
	assertStringSliceAttribute(t, attrs, "gen_ai.response.finish_reasons", []string{"end_turn"})
	// input_tokens folds in cache reads and creations (10 + 7 + 3): Anthropic
	// reports them separately, unlike OpenAI's cache-inclusive prompt_tokens.
	assertInt64Attribute(t, attrs, "gen_ai.usage.input_tokens", 20)
	assertInt64Attribute(t, attrs, "gen_ai.usage.output_tokens", 20)
	assertInt64Attribute(t, attrs, "gen_ai.usage.total_tokens", 40)
	assertInt64Attribute(t, attrs, "gen_ai.usage.cache_read.input_tokens", 7)
	assertInt64Attribute(t, attrs, "gen_ai.usage.cache_creation.input_tokens", 3)
}

// TestOtelMiddleware_Messages_NoCacheUsage verifies that cache attributes are
// omitted when the response reports no prompt-cache activity.
func TestOtelMiddleware_Messages_NoCacheUsage(t *testing.T) {
	sr := setupTestTracer(t)

	middleware := OtelMiddleware()

	reqBody := `{"model":"claude-sonnet-4-5","max_tokens":1024,"messages":[{"role":"user","content":"Hello"}]}`
	req, _ := http.NewRequest(
		"POST",
		"http://api.anthropic.com/v1/messages",
		io.NopCloser(bytes.NewReader([]byte(reqBody))),
	)

	respBody := `{"id":"msg_test_456","type":"message","role":"assistant","model":"claude-sonnet-4-5","stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":20,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}`
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

	attrs := spans[0].Attributes()
	_, found := findAttribute(attrs, "gen_ai.usage.cache_read.input_tokens")
	assert.False(t, found, "cache_read attribute should be omitted when zero")
	_, found = findAttribute(attrs, "gen_ai.usage.cache_creation.input_tokens")
	assert.False(t, found, "cache_creation attribute should be omitted when zero")
}

// TestOtelMiddleware_CountTokens defines the expected span shape for the
// count_tokens endpoint (POST /v1/messages/count_tokens), whose response
// shape ({"input_tokens": N}) differs from the Messages API and carries no
// id, stop_reason, or output/cache usage.
func TestOtelMiddleware_CountTokens(t *testing.T) {
	sr := setupTestTracer(t)

	middleware := OtelMiddleware()

	reqBody := `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"Hello"}]}`
	req, _ := http.NewRequest(
		"POST",
		"http://api.anthropic.com/v1/messages/count_tokens",
		io.NopCloser(bytes.NewReader([]byte(reqBody))),
	)

	next := func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader([]byte(`{"input_tokens":42}`))),
		}, nil
	}

	resp, err := middleware(req, next)
	require.NoError(t, err)
	require.NotNil(t, resp)

	spans := sr.Ended()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, "count_tokens claude-sonnet-4-5", span.Name())

	attrs := span.Attributes()
	assertAttribute(t, attrs, "gen_ai.system", "anthropic")
	assertAttribute(t, attrs, "gen_ai.operation.name", "count_tokens")
	assertAttribute(t, attrs, "gen_ai.request.model", "claude-sonnet-4-5")
	assertAttribute(t, attrs, "gen_ai.provider.name", "anthropic")
	assertInt64Attribute(t, attrs, "gen_ai.usage.input_tokens", 42)
}

// TestOtelMiddleware_StreamingRequestPassThrough verifies that streaming
// requests are not instrumented until event accumulation lands, and that the
// SDK still receives the complete request body.
func TestOtelMiddleware_StreamingRequestPassThrough(t *testing.T) {
	sr := setupTestTracer(t)

	middleware := OtelMiddleware()

	reqBody := `{"model":"claude-sonnet-4-5","max_tokens":1024,"stream":true,"messages":[{"role":"user","content":"Hi"}]}`
	req, _ := http.NewRequest(
		"POST",
		"http://api.anthropic.com/v1/messages",
		io.NopCloser(bytes.NewReader([]byte(reqBody))),
	)

	var received []byte
	next := func(r *http.Request) (*http.Response, error) {
		var err error
		received, err = io.ReadAll(r.Body)
		require.NoError(t, err)
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("event: message_stop\n\n")),
		}, nil
	}

	resp, err := middleware(req, next)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, reqBody, string(received), "SDK must see the full request body")
	assert.Empty(t, sr.Ended(), "streaming requests must not produce spans yet")
}

// TestOtelMiddleware_SSEResponseFallback covers the defensive path where a
// request without the stream flag still receives an SSE response: the span
// ends immediately instead of staying open on an unconsumed body.
func TestOtelMiddleware_SSEResponseFallback(t *testing.T) {
	sr := setupTestTracer(t)

	middleware := OtelMiddleware()

	req, _ := http.NewRequest(
		"POST",
		"http://api.anthropic.com/v1/messages",
		io.NopCloser(bytes.NewReader([]byte(`{"model":"claude-sonnet-4-5","max_tokens":10}`))),
	)

	sse := "event: message_start\ndata: {\"type\":\"message_start\"}\n\n"
	next := func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(sse)),
		}, nil
	}

	resp, err := middleware(req, next)
	require.NoError(t, err)
	require.NotNil(t, resp)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assertBoolAttribute(t, spans[0].Attributes(), "gen_ai.request.is_stream", true)

	// The body is returned untouched for the caller to consume.
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, sse, string(got))
}

// TestOtelMiddleware_RequestBodyReadError verifies that a failing request body
// read still passes a reassembled body to the SDK instead of a truncated one.
func TestOtelMiddleware_RequestBodyReadError(t *testing.T) {
	sr := setupTestTracer(t)

	middleware := OtelMiddleware()

	wantErr := errors.New("read fail")
	req, _ := http.NewRequest(
		"POST",
		"http://api.anthropic.com/v1/messages",
		io.NopCloser(iotest.ErrReader(wantErr)),
	)

	called := false
	next := func(r *http.Request) (*http.Response, error) {
		called = true
		// The reassembled body must surface the original read error rather
		// than appear as a silently truncated payload.
		_, err := io.ReadAll(r.Body)
		require.ErrorIs(t, err, wantErr)
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	}

	_, err := middleware(req, next)
	require.NoError(t, err)
	assert.True(t, called)
	assert.Empty(t, sr.Ended())
}

func TestOtelMiddleware_NilResponseBody(t *testing.T) {
	sr := setupTestTracer(t)

	middleware := OtelMiddleware()

	req, _ := http.NewRequest(
		"POST",
		"http://api.anthropic.com/v1/messages",
		io.NopCloser(bytes.NewReader([]byte(`{"model":"claude-sonnet-4-5","max_tokens":10}`))),
	)

	next := func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{},
			Body:       nil,
		}, nil
	}

	resp, err := middleware(req, next)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// No panic, and the span still ends.
	require.Len(t, sr.Ended(), 1)
}

// TestOtelMiddleware_ResponseBodyReadError verifies the span still ends and the
// caller gets a reassembled body when the response read fails mid-parse.
func TestOtelMiddleware_ResponseBodyReadError(t *testing.T) {
	sr := setupTestTracer(t)

	middleware := OtelMiddleware()

	req, _ := http.NewRequest(
		"POST",
		"http://api.anthropic.com/v1/messages",
		io.NopCloser(bytes.NewReader([]byte(`{"model":"claude-sonnet-4-5","max_tokens":10}`))),
	)

	wantErr := errors.New("response read fail")
	next := func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(iotest.ErrReader(wantErr)),
		}, nil
	}

	resp, err := middleware(req, next)
	require.NoError(t, err)
	require.NotNil(t, resp)

	require.Len(t, sr.Ended(), 1)

	// The reassembled body surfaces the original error to the caller.
	_, err = io.ReadAll(resp.Body)
	require.ErrorIs(t, err, wantErr)
}

func TestOtelMiddleware_HTTPError(t *testing.T) {
	tests := []struct {
		name        string
		statusCode  int
		status      string
		contentType string
	}{
		{"rate limit", 429, "429 Too Many Requests", "application/json"},
		{"server error", 500, "500 Internal Server Error", "application/json"},
		{"streaming error", 500, "500 Internal Server Error", "text/event-stream"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := setupTestTracer(t)

			middleware := OtelMiddleware()

			req, _ := http.NewRequest(
				"POST",
				"http://api.anthropic.com/v1/messages",
				io.NopCloser(bytes.NewReader([]byte(`{"model":"claude-sonnet-4-5","max_tokens":10}`))),
			)

			next := func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: tt.statusCode,
					Status:     tt.status,
					Header:     http.Header{"Content-Type": []string{tt.contentType}},
					Body:       io.NopCloser(strings.NewReader("")),
				}, nil
			}

			resp, err := middleware(req, next)
			require.NoError(t, err)
			require.NotNil(t, resp)

			spans := sr.Ended()
			require.Len(t, spans, 1)
			assertAttribute(t, spans[0].Attributes(), "error.type", tt.status)
			assert.Equal(t, codes.Error, spans[0].Status().Code)

			events := spans[0].Events()
			require.Len(t, events, 1, "expected exception event for HTTP error status")
			assert.Equal(t, "exception", events[0].Name)
		})
	}
}

func TestOtelMiddleware_TransportError(t *testing.T) {
	sr := setupTestTracer(t)

	middleware := OtelMiddleware()

	req, _ := http.NewRequest(
		"POST",
		"http://api.anthropic.com/v1/messages",
		io.NopCloser(bytes.NewReader([]byte(`{"model":"claude-sonnet-4-5","max_tokens":10}`))),
	)

	wantErr := errors.New("connection refused")
	next := func(r *http.Request) (*http.Response, error) {
		return nil, wantErr
	}

	resp, err := middleware(req, next)
	require.ErrorIs(t, err, wantErr)
	assert.Nil(t, resp)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status().Code)

	events := spans[0].Events()
	require.Len(t, events, 1, "expected exception event for transport error")
	assert.Equal(t, "exception", events[0].Name)

	assertAttribute(t, spans[0].Attributes(), "error.type", "*errors.errorString")
}

func TestOtelMiddleware_SkipsNilBody(t *testing.T) {
	sr := setupTestTracer(t)

	middleware := OtelMiddleware()

	req, _ := http.NewRequest("POST", "http://api.anthropic.com/v1/messages", nil)

	called := false
	next := func(r *http.Request) (*http.Response, error) {
		called = true
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	}

	_, err := middleware(req, next)
	require.NoError(t, err)
	assert.True(t, called)
	assert.Empty(t, sr.Ended())
}

func TestOtelMiddleware_InvalidResponseJSON(t *testing.T) {
	sr := setupTestTracer(t)

	middleware := OtelMiddleware()

	req, _ := http.NewRequest(
		"POST",
		"http://api.anthropic.com/v1/messages",
		io.NopCloser(bytes.NewReader([]byte(`{"model":"claude-sonnet-4-5","max_tokens":10}`))),
	)

	next := func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader("not json")),
		}, nil
	}

	resp, err := middleware(req, next)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// The span still ends; it just carries no response attributes.
	spans := sr.Ended()
	require.Len(t, spans, 1)
}

func findAttribute(attrs []attribute.KeyValue, key string) (attribute.Value, bool) {
	for _, kv := range attrs {
		if string(kv.Key) == key {
			return kv.Value, true
		}
	}
	return attribute.Value{}, false
}

func assertAttribute(t *testing.T, attrs []attribute.KeyValue, key, expected string) {
	t.Helper()
	val, ok := findAttribute(attrs, key)
	require.True(t, ok, "attribute %s not found", key)
	assert.Equal(t, expected, val.AsString(), "attribute %s", key)
}

func assertInt64Attribute(t *testing.T, attrs []attribute.KeyValue, key string, expected int64) {
	t.Helper()
	val, ok := findAttribute(attrs, key)
	require.True(t, ok, "attribute %s not found", key)
	assert.Equal(t, expected, val.AsInt64(), "attribute %s", key)
}

func assertFloat64Attribute(t *testing.T, attrs []attribute.KeyValue, key string, expected float64) {
	t.Helper()
	val, ok := findAttribute(attrs, key)
	require.True(t, ok, "attribute %s not found", key)
	assert.InDelta(t, expected, val.AsFloat64(), 1e-9, "attribute %s", key)
}

func assertStringSliceAttribute(t *testing.T, attrs []attribute.KeyValue, key string, expected []string) {
	t.Helper()
	val, ok := findAttribute(attrs, key)
	require.True(t, ok, "attribute %s not found", key)
	assert.Equal(t, expected, val.AsStringSlice(), "attribute %s", key)
}

func assertBoolAttribute(t *testing.T, attrs []attribute.KeyValue, key string, expected bool) {
	t.Helper()
	val, ok := findAttribute(attrs, key)
	require.True(t, ok, "attribute %s not found", key)
	assert.Equal(t, expected, val.AsBool(), "attribute %s", key)
}
