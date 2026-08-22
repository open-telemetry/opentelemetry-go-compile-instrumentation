// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package genai

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// newRecordingTracer points the package tracer at an in-memory exporter and
// returns it. initInstrumentation is guarded by a sync.Once that a test must
// not race with, so the tracer is assigned directly.
func newRecordingTracer(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := tracer
	tracer = provider.Tracer("test")
	t.Cleanup(func() { tracer = previous })

	return recorder
}

// attrsOf indexes a recorded span's attributes by key.
func attrsOf(t *testing.T, span sdktrace.ReadOnlySpan) map[attribute.Key]attribute.Value {
	t.Helper()

	out := make(map[attribute.Key]attribute.Value, len(span.Attributes()))
	for _, kv := range span.Attributes() {
		out[kv.Key] = kv.Value
	}
	return out
}

func TestClassifyOperation(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		wantOp    operationType
		wantModel string
	}{
		{
			name:      "gemini api generate content",
			path:      "/v1beta/models/gemini-2.5-flash:generateContent",
			wantOp:    opChat,
			wantModel: "gemini-2.5-flash",
		},
		{
			name:      "vertex ai generate content",
			path:      "/v1beta1/projects/p/locations/l/publishers/google/models/gemini-2.5-flash:generateContent",
			wantOp:    opChat,
			wantModel: "gemini-2.5-flash",
		},
		{
			name:      "tuned model",
			path:      "/v1beta/tunedModels/my-tuned-model:generateContent",
			wantOp:    opChat,
			wantModel: "my-tuned-model",
		},
		{
			name:      "streaming",
			path:      "/v1beta/models/gemini-2.5-flash:streamGenerateContent",
			wantOp:    opStreamChat,
			wantModel: "gemini-2.5-flash",
		},
		{
			name:   "count tokens is out of scope",
			path:   "/v1beta/models/gemini-2.5-flash:countTokens",
			wantOp: opUnknown,
		},
		{
			name:   "embed content is out of scope",
			path:   "/v1beta/models/text-embedding-004:embedContent",
			wantOp: opUnknown,
		},
		{
			name:   "no rpc suffix",
			path:   "/v1beta/models",
			wantOp: opUnknown,
		},
		{
			name:   "empty model",
			path:   "/v1beta/models/:generateContent",
			wantOp: opUnknown,
		},
		{
			name:   "empty path",
			path:   "",
			wantOp: opUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op, model := classifyOperation(tt.path)
			assert.Equal(t, tt.wantOp, op)
			assert.Equal(t, tt.wantModel, model)
		})
	}
}

// newBodyRequest mirrors how the SDK builds its requests: http.NewRequest over
// an in-memory reader, which is what populates GetBody. httptest.NewRequest
// does not set GetBody, so it cannot stand in here.
func newBodyRequest(t *testing.T, body string) *http.Request {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		"https://generativelanguage.googleapis.com/v1beta/models/m:generateContent",
		strings.NewReader(body))
	require.NoError(t, err)
	require.NotNil(t, req.GetBody, "http.NewRequest must expose a replayable body")

	return req
}

func TestRequestConfigAttrs(t *testing.T) {
	req := newBodyRequest(t, `{"contents":[{"role":"user","parts":[{"text":"hi"}]}],`+
		`"generationConfig":{"temperature":0.5,"topP":0.9,"topK":40,"maxOutputTokens":256}}`)

	attrs := requestConfigAttrs(req)

	require.Len(t, attrs, 4)
	byKey := make(map[attribute.Key]attribute.Value, len(attrs))
	for _, kv := range attrs {
		byKey[kv.Key] = kv.Value
	}
	assert.Equal(t, int64(256), byKey["gen_ai.request.max_tokens"].AsInt64())
	assert.InDelta(t, 0.5, byKey["gen_ai.request.temperature"].AsFloat64(), 1e-9)
	assert.InDelta(t, 0.9, byKey["gen_ai.request.top_p"].AsFloat64(), 1e-9)
	assert.Equal(t, int64(40), byKey["gen_ai.request.top_k"].AsInt64())
}

func TestRequestConfigAttrs_NoGenerationConfig(t *testing.T) {
	assert.Empty(t, requestConfigAttrs(newBodyRequest(t, `{"contents":[]}`)))
}

func TestRequestConfigAttrs_UnreadableBody(t *testing.T) {
	// A request whose body cannot be replayed yields no attributes rather than
	// consuming the body the server still has to read.
	req := newBodyRequest(t, `{}`)
	req.GetBody = nil
	assert.Empty(t, requestConfigAttrs(req))

	req = newBodyRequest(t, `{}`)
	req.GetBody = func() (io.ReadCloser, error) { return nil, errors.New("boom") }
	assert.Empty(t, requestConfigAttrs(req))
}

func TestRequestConfigAttrs_InvalidJSON(t *testing.T) {
	assert.Empty(t, requestConfigAttrs(newBodyRequest(t, `not json`)))
}

func TestResponseAttrs(t *testing.T) {
	body := []byte(`{
		"responseId": "resp-123",
		"modelVersion": "gemini-2.5-flash-001",
		"candidates": [{"finishReason": "STOP"}],
		"usageMetadata": {
			"promptTokenCount": 10,
			"candidatesTokenCount": 20,
			"totalTokenCount": 30
		}
	}`)

	byKey := make(map[attribute.Key]attribute.Value)
	for _, kv := range responseAttrs(body) {
		byKey[kv.Key] = kv.Value
	}

	assert.Equal(t, "resp-123", byKey["gen_ai.response.id"].AsString())
	assert.Equal(t, "gemini-2.5-flash-001", byKey["gen_ai.response.model"].AsString())
	assert.Equal(t, []string{"STOP"}, byKey["gen_ai.response.finish_reasons"].AsStringSlice())
	assert.Equal(t, int64(10), byKey["gen_ai.usage.input_tokens"].AsInt64())
	assert.Equal(t, int64(20), byKey["gen_ai.usage.output_tokens"].AsInt64())
	assert.Equal(t, int64(30), byKey["gen_ai.usage.total_tokens"].AsInt64())
}

func TestResponseAttrs_ThoughtsCountAsOutput(t *testing.T) {
	// Thinking tokens are generated by the model but reported apart from
	// candidatesTokenCount, so output_tokens must include them.
	body := []byte(`{
		"usageMetadata": {
			"promptTokenCount": 10,
			"candidatesTokenCount": 20,
			"thoughtsTokenCount": 7,
			"totalTokenCount": 37
		}
	}`)

	byKey := make(map[attribute.Key]attribute.Value)
	for _, kv := range responseAttrs(body) {
		byKey[kv.Key] = kv.Value
	}

	assert.Equal(t, int64(27), byKey["gen_ai.usage.output_tokens"].AsInt64())
	assert.Equal(t, int64(37), byKey["gen_ai.usage.total_tokens"].AsInt64())
}

func TestResponseAttrs_DerivesMissingTotal(t *testing.T) {
	body := []byte(`{"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":20}}`)

	byKey := make(map[attribute.Key]attribute.Value)
	for _, kv := range responseAttrs(body) {
		byKey[kv.Key] = kv.Value
	}

	assert.Equal(t, int64(30), byKey["gen_ai.usage.total_tokens"].AsInt64())
}

func TestResponseAttrs_MultipleCandidates(t *testing.T) {
	body := []byte(`{"candidates":[{"finishReason":"STOP"},{"finishReason":"MAX_TOKENS"},{}]}`)

	byKey := make(map[attribute.Key]attribute.Value)
	for _, kv := range responseAttrs(body) {
		byKey[kv.Key] = kv.Value
	}

	assert.Equal(t, []string{"STOP", "MAX_TOKENS"}, byKey["gen_ai.response.finish_reasons"].AsStringSlice())
}

func TestResponseAttrs_NoUsage(t *testing.T) {
	attrs := responseAttrs([]byte(`{"responseId":"resp-1"}`))

	require.Len(t, attrs, 1)
	assert.Equal(t, attribute.Key("gen_ai.response.id"), attrs[0].Key)
}

func TestResponseAttrs_InvalidJSON(t *testing.T) {
	assert.Nil(t, responseAttrs([]byte(`{`)))
}

// roundTripThrough drives the transport against a test server and returns the
// single recorded span plus the body the caller reads back.
func roundTripThrough(t *testing.T, handler http.HandlerFunc, reqBody string) (sdktrace.ReadOnlySpan, string) {
	t.Helper()

	recorder := newRecordingTracer(t)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	transport := &otelTransport{provider: providerGemini, system: systemGemini}
	client := &http.Client{Transport: transport}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		server.URL+"/v1beta/models/gemini-2.5-flash:generateContent", strings.NewReader(reqBody))
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	spans := recorder.Ended()
	require.Len(t, spans, 1)

	return spans[0], string(got)
}

func TestRoundTrip_EmitsGenAISpan(t *testing.T) {
	const respBody = `{"responseId":"resp-1","modelVersion":"gemini-2.5-flash-001",` +
		`"candidates":[{"finishReason":"STOP","content":{"parts":[{"text":"hello"}]}}],` +
		`"usageMetadata":{"promptTokenCount":11,"candidatesTokenCount":22,"totalTokenCount":33}}`

	span, body := roundTripThrough(t, func(w http.ResponseWriter, r *http.Request) {
		// The instrumentation must leave the request body intact for the server.
		sent, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		assert.Contains(t, string(sent), `"maxOutputTokens":64`)

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, respBody)
	}, `{"contents":[],"generationConfig":{"maxOutputTokens":64}}`)

	// The response body must survive being parsed for attributes.
	assert.JSONEq(t, respBody, body)

	assert.Equal(t, "chat gemini-2.5-flash", span.Name())
	assert.Equal(t, trace.SpanKindClient, span.SpanKind())
	assert.Equal(t, codes.Unset, span.Status().Code)

	attrs := attrsOf(t, span)
	assert.Equal(t, "gemini", attrs["gen_ai.system"].AsString())
	assert.Equal(t, "gcp.gemini", attrs["gen_ai.provider.name"].AsString())
	assert.Equal(t, "chat", attrs["gen_ai.operation.name"].AsString())
	assert.Equal(t, "gemini-2.5-flash", attrs["gen_ai.request.model"].AsString())
	assert.Equal(t, int64(64), attrs["gen_ai.request.max_tokens"].AsInt64())
	assert.Equal(t, "resp-1", attrs["gen_ai.response.id"].AsString())
	assert.Equal(t, "gemini-2.5-flash-001", attrs["gen_ai.response.model"].AsString())
	assert.Equal(t, []string{"STOP"}, attrs["gen_ai.response.finish_reasons"].AsStringSlice())
	assert.Equal(t, int64(11), attrs["gen_ai.usage.input_tokens"].AsInt64())
	assert.Equal(t, int64(22), attrs["gen_ai.usage.output_tokens"].AsInt64())
	assert.Equal(t, int64(33), attrs["gen_ai.usage.total_tokens"].AsInt64())
}

func TestRoundTrip_HTTPErrorSetsErrorStatus(t *testing.T) {
	span, _ := roundTripThrough(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "quota exceeded", http.StatusTooManyRequests)
	}, `{}`)

	assert.Equal(t, codes.Error, span.Status().Code)

	attrs := attrsOf(t, span)
	assert.Equal(t, "429", attrs["error.type"].AsString())
	// A failed call carries no response attributes.
	assert.NotContains(t, attrs, attribute.Key("gen_ai.usage.input_tokens"))
}

func TestRoundTrip_SSEResponseMarkedAsStream(t *testing.T) {
	span, _ := roundTripThrough(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {}\n\n")
	}, `{}`)

	attrs := attrsOf(t, span)
	assert.True(t, attrs["gen_ai.request.is_stream"].AsBool())
	assert.NotContains(t, attrs, attribute.Key("gen_ai.usage.input_tokens"))
}

func TestRoundTrip_TransportErrorRecorded(t *testing.T) {
	recorder := newRecordingTracer(t)

	wantErr := errors.New("dial failed")
	transport := &otelTransport{
		base:     roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, wantErr }),
		provider: providerGemini,
		system:   systemGemini,
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		"https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent",
		strings.NewReader(`{}`))
	require.NoError(t, err)

	_, err = transport.RoundTrip(req)
	require.ErrorIs(t, err, wantErr)

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status().Code)
	require.Len(t, spans[0].Events(), 1)
	assert.Equal(t, "exception", spans[0].Events()[0].Name)
}

func TestRoundTrip_UninstrumentedPathsPassThrough(t *testing.T) {
	recorder := newRecordingTracer(t)

	var seen int
	transport := &otelTransport{
		base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			seen++
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(nil)),
				Header:     http.Header{},
			}, nil
		}),
		provider: providerGemini,
		system:   systemGemini,
	}

	for _, path := range []string{
		"/v1beta/models/gemini-2.5-flash:countTokens",
		"/v1beta/models/gemini-2.5-flash:streamGenerateContent",
		"/v1beta/models",
	} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
			"https://generativelanguage.googleapis.com"+path, strings.NewReader(`{}`))
		require.NoError(t, err)

		resp, err := transport.RoundTrip(req)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
	}

	assert.Equal(t, 3, seen, "every request must still reach the wrapped transport")
	assert.Empty(t, recorder.Ended(), "out-of-scope calls must not create spans")
}

func TestRoundTripper_DefaultsWhenBaseIsNil(t *testing.T) {
	// The SDK leaves http.Client.Transport nil to mean "use the default", so
	// the wrapper must resolve that rather than dereference nil.
	assert.Same(t, http.DefaultTransport, (&otelTransport{}).roundTripper())

	called := false
	base := roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: http.Header{}}, nil
	})

	resp, err := (&otelTransport{base: base}).roundTripper().RoundTrip(newBodyRequest(t, `{}`))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.True(t, called, "a configured base transport must be used")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
