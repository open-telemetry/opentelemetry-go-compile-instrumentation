// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package v3

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestStreamingReader_ChatChunks(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tr := tp.Tracer("test")
	ctx, span := tr.Start(t.Context(), "test-stream")

	streamData := "data: {\"id\":\"chatcmpl-abc\",\"model\":\"gpt-4\",\"choices\":[{\"delta\":{\"content\":\"Hi\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"chatcmpl-abc\",\"model\":\"gpt-4\",\"choices\":[{\"delta\":{\"content\":\" there\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":3,\"total_tokens\":11}}\n\ndata: [DONE]\n\n"

	body := io.NopCloser(bytes.NewReader([]byte(streamData)))
	reader := newStreamingReader(body, span, time.Now(), "gpt-4", "chat", "openai", opChat, ctx)

	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Contains(t, string(data), "chatcmpl-abc")

	err = reader.Close()
	require.NoError(t, err)

	spans := sr.Ended()
	require.Len(t, spans, 1)

	s := spans[0]
	attrs := s.Attributes()
	assertAttribute(t, attrs, "gen_ai.response.id", "chatcmpl-abc")
	assertAttribute(t, attrs, "gen_ai.response.model", "gpt-4")
	assertInt64Attribute(t, attrs, "gen_ai.usage.input_tokens", 8)
	assertInt64Attribute(t, attrs, "gen_ai.usage.output_tokens", 3)
	assertInt64Attribute(t, attrs, "gen_ai.usage.total_tokens", 11)
}

func TestStreamingReader_CompletionChunks(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tr := tp.Tracer("test")
	ctx, span := tr.Start(t.Context(), "test-completion-stream")

	streamData := "data: {\"id\":\"cmpl-xyz\",\"model\":\"gpt-3.5-turbo-instruct\",\"choices\":[{\"text\":\"Hello\",\"finish_reason\":\"length\"}],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":10,\"total_tokens\":14}}\n\ndata: [DONE]\n\n"

	body := io.NopCloser(bytes.NewReader([]byte(streamData)))
	reader := newStreamingReader(body, span, time.Now(), "gpt-3.5-turbo-instruct", "text_completion", "openai", opCompletion, ctx)

	_, err := io.ReadAll(reader)
	require.NoError(t, err)
	reader.Close()

	spans := sr.Ended()
	require.Len(t, spans, 1)

	attrs := spans[0].Attributes()
	assertAttribute(t, attrs, "gen_ai.response.id", "cmpl-xyz")
	assertAttribute(t, attrs, "gen_ai.response.model", "gpt-3.5-turbo-instruct")
	assertInt64Attribute(t, attrs, "gen_ai.usage.input_tokens", 4)
	assertInt64Attribute(t, attrs, "gen_ai.usage.output_tokens", 10)
	assertInt64Attribute(t, attrs, "gen_ai.usage.total_tokens", 14)
}

func TestStreamingReader_EmptyStream(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tr := tp.Tracer("test")
	ctx, span := tr.Start(t.Context(), "test-empty-stream")

	body := io.NopCloser(bytes.NewReader([]byte("")))
	reader := newStreamingReader(body, span, time.Now(), "gpt-4", "chat", "openai", opChat, ctx)

	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Empty(t, data)
	reader.Close()

	spans := sr.Ended()
	require.Len(t, spans, 1)
}

func TestStreamingReader_CloseBeforeRead(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tr := tp.Tracer("test")
	ctx, span := tr.Start(t.Context(), "test-close-early")

	streamData := "data: {\"id\":\"early\",\"model\":\"gpt-4\",\"choices\":[]}\n\ndata: [DONE]\n\n"
	body := io.NopCloser(bytes.NewReader([]byte(streamData)))
	reader := newStreamingReader(body, span, time.Now(), "gpt-4", "chat", "openai", opChat, ctx)

	err := reader.Close()
	require.NoError(t, err)

	spans := sr.Ended()
	require.Len(t, spans, 1)
}

func TestStreamingReader_MultipleCloseIdempotent(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tr := tp.Tracer("test")
	ctx, span := tr.Start(t.Context(), "test-multi-close")

	body := io.NopCloser(bytes.NewReader([]byte("data: [DONE]\n\n")))
	reader := newStreamingReader(body, span, time.Now(), "gpt-4", "chat", "openai", opChat, ctx)

	_, _ = io.ReadAll(reader)
	reader.Close()
	reader.Close() // second close should not panic

	spans := sr.Ended()
	require.Len(t, spans, 1, "span should only be ended once")
}

func TestStreamingReader_FinishReasons(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tr := tp.Tracer("test")
	ctx, span := tr.Start(t.Context(), "test-reasons")

	streamData := "data: {\"id\":\"r1\",\"model\":\"gpt-4\",\"choices\":[{\"delta\":{\"content\":\"a\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	body := io.NopCloser(bytes.NewReader([]byte(streamData)))
	reader := newStreamingReader(body, span, time.Now(), "gpt-4", "chat", "openai", opChat, ctx)

	_, _ = io.ReadAll(reader)
	reader.Close()

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assertSliceAttribute(t, spans[0].Attributes(), "gen_ai.response.finish_reasons", []string{"stop"})
}

func TestStreamingReader_FirstTokenLatency(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tr := tp.Tracer("test")
	ctx, span := tr.Start(t.Context(), "test-latency")

	start := time.Now().Add(-100 * time.Millisecond) // simulate 100ms delay
	streamData := "data: {\"id\":\"lat\",\"model\":\"gpt-4\",\"choices\":[{\"delta\":{\"content\":\"x\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	body := io.NopCloser(bytes.NewReader([]byte(streamData)))
	reader := newStreamingReader(body, span, start, "gpt-4", "chat", "openai", opChat, ctx)

	_, _ = io.ReadAll(reader)
	reader.Close()

	spans := sr.Ended()
	require.Len(t, spans, 1)
	// Verify time_to_first_token attribute exists
	hasTimeToFirst := false
	for _, attr := range spans[0].Attributes() {
		if attr.Key == "gen_ai.response.time_to_first_token" {
			hasTimeToFirst = true
			assert.Greater(t, attr.Value.AsInt64(), int64(0))
		}
	}
	assert.True(t, hasTimeToFirst, "should have time_to_first_token attribute")
}

func TestParseSSELine(t *testing.T) {
	tests := []struct {
		name     string
		line     []byte
		payload  []byte
		isDone   bool
	}{
		{"data line", []byte("data: {\"id\":\"1\"}"), []byte("{\"id\":\"1\"}"), false},
		{"done signal", []byte("data: [DONE]"), nil, true},
		{"non-data line", []byte(": comment"), nil, false},
		{"empty prefix", []byte("event: message"), nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, done := parseSSELine(tt.line)
			assert.Equal(t, tt.isDone, done)
			if tt.payload != nil {
				assert.Equal(t, tt.payload, payload)
			} else {
				assert.Nil(t, payload)
			}
		})
	}
}

func TestStreamingReader_IncrementalRead(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tr := tp.Tracer("test")
	ctx, span := tr.Start(t.Context(), "test-incremental")

	streamData := "data: {\"id\":\"inc\",\"model\":\"gpt-4\",\"choices\":[{\"delta\":{\"content\":\"a\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"inc\",\"model\":\"gpt-4\",\"choices\":[{\"delta\":{\"content\":\"b\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":2,\"total_tokens\":3}}\n\ndata: [DONE]\n\n"
	body := io.NopCloser(bytes.NewReader([]byte(streamData)))
	reader := newStreamingReader(body, span, time.Now(), "gpt-4", "chat", "openai", opChat, ctx)

	// Read in small chunks to test incremental processing
	buf := make([]byte, 10)
	var total int
	for {
		n, err := reader.Read(buf)
		total += n
		if err != nil {
			break
		}
	}
	assert.Greater(t, total, 0)
	reader.Close()

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assertAttribute(t, spans[0].Attributes(), "gen_ai.response.id", "inc")
}

// Helper functions for attribute assertions.

func assertAttribute(t *testing.T, attrs []attribute.KeyValue, key, expected string) {
	t.Helper()
	for _, attr := range attrs {
		if string(attr.Key) == key {
			if attr.Value.Type() == attribute.BOOL {
				assert.Equal(t, expected, boolString(attr.Value.AsBool()))
				return
			}
			assert.Equal(t, expected, attr.Value.AsString())
			return
		}
	}
	t.Errorf("attribute %q not found", key)
}

func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func assertInt64Attribute(t *testing.T, attrs []attribute.KeyValue, key string, expected int64) {
	t.Helper()
	for _, attr := range attrs {
		if string(attr.Key) == key {
			assert.Equal(t, expected, attr.Value.AsInt64())
			return
		}
	}
	t.Errorf("attribute %q not found", key)
}

func assertFloat64Attribute(t *testing.T, attrs []attribute.KeyValue, key string, expected float64) {
	t.Helper()
	for _, attr := range attrs {
		if string(attr.Key) == key {
			assert.InDelta(t, expected, attr.Value.AsFloat64(), 0.001)
			return
		}
	}
	t.Errorf("attribute %q not found", key)
}

func assertSliceAttribute(t *testing.T, attrs []attribute.KeyValue, key string, expected []string) {
	t.Helper()
	for _, attr := range attrs {
		if string(attr.Key) == key {
			assert.Equal(t, expected, attr.Value.AsStringSlice())
			return
		}
	}
	t.Errorf("attribute %q not found", key)
}
