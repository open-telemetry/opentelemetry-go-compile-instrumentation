// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package streaming

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestStreamingReader_ChatChunks(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tr := tp.Tracer("test")
	_, span := tr.Start(t.Context(), "test-stream")

	streamData := "data: {\"id\":\"chatcmpl-abc\",\"model\":\"gpt-4\",\"choices\":[{\"delta\":{\"content\":\"Hi\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"chatcmpl-abc\",\"model\":\"gpt-4\",\"choices\":[{\"delta\":{\"content\":\" there\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":3,\"total_tokens\":11}}\n\ndata: [DONE]\n\n"

	body := io.NopCloser(bytes.NewReader([]byte(streamData)))
	reader := NewStreamingReader(body, span, time.Now(), OpChat, false, 0)

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
	_, span := tr.Start(t.Context(), "test-completion-stream")

	streamData := "data: {\"id\":\"cmpl-xyz\",\"model\":\"gpt-3.5-turbo-instruct\",\"choices\":[{\"text\":\"Hello\",\"finish_reason\":\"length\"}],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":10,\"total_tokens\":14}}\n\ndata: [DONE]\n\n"

	body := io.NopCloser(bytes.NewReader([]byte(streamData)))
	reader := NewStreamingReader(body, span, time.Now(), OpCompletion, false, 0)

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
	_, span := tr.Start(t.Context(), "test-empty-stream")

	body := io.NopCloser(bytes.NewReader([]byte("")))
	reader := NewStreamingReader(body, span, time.Now(), OpChat, false, 0)

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
	_, span := tr.Start(t.Context(), "test-close-early")

	streamData := "data: {\"id\":\"early\",\"model\":\"gpt-4\",\"choices\":[]}\n\ndata: [DONE]\n\n"
	body := io.NopCloser(bytes.NewReader([]byte(streamData)))
	reader := NewStreamingReader(body, span, time.Now(), OpChat, false, 0)

	err := reader.Close()
	require.NoError(t, err)

	spans := sr.Ended()
	require.Len(t, spans, 1)
}

func TestStreamingReader_MultipleCloseIdempotent(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tr := tp.Tracer("test")
	_, span := tr.Start(t.Context(), "test-multi-close")

	body := io.NopCloser(bytes.NewReader([]byte("data: [DONE]\n\n")))
	reader := NewStreamingReader(body, span, time.Now(), OpChat, false, 0)

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
	_, span := tr.Start(t.Context(), "test-reasons")

	streamData := "data: {\"id\":\"r1\",\"model\":\"gpt-4\",\"choices\":[{\"delta\":{\"content\":\"a\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	body := io.NopCloser(bytes.NewReader([]byte(streamData)))
	reader := NewStreamingReader(body, span, time.Now(), OpChat, false, 0)

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
	_, span := tr.Start(t.Context(), "test-latency")

	start := time.Now().Add(-100 * time.Millisecond) // simulate 100ms delay
	streamData := "data: {\"id\":\"lat\",\"model\":\"gpt-4\",\"choices\":[{\"delta\":{\"content\":\"x\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	body := io.NopCloser(bytes.NewReader([]byte(streamData)))
	reader := NewStreamingReader(body, span, start, OpChat, false, 0)

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

// eofWithFinalChunkReader returns the given data and io.EOF from the same
// Read call, with no trailing newline after the data. This mirrors what a
// lot of real io.Readers do once the underlying connection is closed right
// after the last chunk.
type eofWithFinalChunkReader struct {
	data []byte
	sent bool
}

func (r *eofWithFinalChunkReader) Read(p []byte) (int, error) {
	if r.sent {
		return 0, io.EOF
	}
	r.sent = true
	n := copy(p, r.data)
	return n, io.EOF
}

func (r *eofWithFinalChunkReader) Close() error {
	return nil
}

func TestStreamingReader_FinalChunkWithoutTrailingNewline(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tr := tp.Tracer("test")
	_, span := tr.Start(t.Context(), "test-no-trailing-newline")

	// No trailing "\n" after this chunk, and no separate "[DONE]" line.
	streamData := "data: {\"id\":\"final\",\"model\":\"gpt-4\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}"

	body := &eofWithFinalChunkReader{data: []byte(streamData)}
	reader := NewStreamingReader(body, span, time.Now(), OpChat, false, 0)

	_, _ = io.ReadAll(reader)
	reader.Close()

	spans := sr.Ended()
	require.Len(t, spans, 1)

	attrs := spans[0].Attributes()
	assertAttribute(t, attrs, "gen_ai.response.id", "final")
	assertInt64Attribute(t, attrs, "gen_ai.usage.output_tokens", 5)
	assertSliceAttribute(t, attrs, "gen_ai.response.finish_reasons", []string{"stop"})
}

func TestStreamingReader_FinalChunkWithoutTrailingNewline_Completion(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tr := tp.Tracer("test")
	_, span := tr.Start(t.Context(), "test-no-trailing-newline-completion")

	// No trailing "\n" after this chunk, and no separate "[DONE]" line.
	streamData := "data: {\"id\":\"cmpl-final\",\"model\":\"gpt-3.5-turbo-instruct\",\"choices\":[{\"text\":\"Hi\",\"finish_reason\":\"length\"}],\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":4,\"total_tokens\":13}}"

	body := &eofWithFinalChunkReader{data: []byte(streamData)}
	reader := NewStreamingReader(body, span, time.Now(), OpCompletion, false, 0)

	_, _ = io.ReadAll(reader)
	reader.Close()

	spans := sr.Ended()
	require.Len(t, spans, 1)

	attrs := spans[0].Attributes()
	assertAttribute(t, attrs, "gen_ai.response.id", "cmpl-final")
	assertInt64Attribute(t, attrs, "gen_ai.usage.output_tokens", 4)
	assertSliceAttribute(t, attrs, "gen_ai.response.finish_reasons", []string{"length"})
}

// dataThenEOFReader hands back its payload with a nil error. A caller that
// stops there never triggers the Read path's flush, because that only runs
// once Read reports a non-nil error.
type dataThenEOFReader struct {
	data []byte
	sent bool
}

func (r *dataThenEOFReader) Read(p []byte) (int, error) {
	if r.sent {
		return 0, io.EOF
	}
	r.sent = true
	n := copy(p, r.data)
	return n, nil
}

func (r *dataThenEOFReader) Close() error {
	return nil
}

// A caller that reads what it needs and closes without draining to EOF must
// still get the final chunk, so Close has to flush the line buffer for the
// same reason Read does.
func TestStreamingReader_CloseWithoutDrainingFlushesFinalChunk(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tr := tp.Tracer("test")
	_, span := tr.Start(t.Context(), "test-close-without-draining")

	// No trailing "\n" and no "[DONE]" line, so this chunk only leaves the
	// line buffer if something flushes it explicitly.
	streamData := "data: {\"id\":\"closed-early\",\"model\":\"gpt-4\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":2,\"total_tokens\":9}}"

	body := &dataThenEOFReader{data: []byte(streamData)}
	reader := NewStreamingReader(body, span, time.Now(), OpChat, false, 0)

	buf := make([]byte, len(streamData))
	n, err := reader.Read(buf)
	require.NoError(t, err)
	require.Equal(t, len(streamData), n)

	require.NoError(t, reader.Close())

	spans := sr.Ended()
	require.Len(t, spans, 1)

	attrs := spans[0].Attributes()
	assertAttribute(t, attrs, "gen_ai.response.id", "closed-early")
	assertInt64Attribute(t, attrs, "gen_ai.usage.input_tokens", 7)
	assertInt64Attribute(t, attrs, "gen_ai.usage.output_tokens", 2)
	assertSliceAttribute(t, attrs, "gen_ai.response.finish_reasons", []string{"stop"})
}

func TestParseSSELine(t *testing.T) {
	tests := []struct {
		name    string
		line    []byte
		payload []byte
		isDone  bool
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
	_, span := tr.Start(t.Context(), "test-incremental")

	streamData := "data: {\"id\":\"inc\",\"model\":\"gpt-4\",\"choices\":[{\"delta\":{\"content\":\"a\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"inc\",\"model\":\"gpt-4\",\"choices\":[{\"delta\":{\"content\":\"b\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":2,\"total_tokens\":3}}\n\ndata: [DONE]\n\n"
	body := io.NopCloser(bytes.NewReader([]byte(streamData)))
	reader := NewStreamingReader(body, span, time.Now(), OpChat, false, 0)

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
			assert.Equal(t, expected, attr.Value.AsString())
			return
		}
	}
	t.Errorf("attribute %q not found", key)
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

func TestStreamingReader_ContentCapture(t *testing.T) {

	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tr := tp.Tracer("test")
	_, span := tr.Start(t.Context(), "test-content")

	streamData := "data: {\"id\":\"inc\",\"model\":\"gpt-4\",\"choices\":[{\"delta\":{\"content\":\"hello \"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"inc\",\"model\":\"gpt-4\",\"choices\":[{\"delta\":{\"content\":\"world\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"

	body := io.NopCloser(bytes.NewReader([]byte(streamData)))
	reader := NewStreamingReader(body, span, time.Now(), OpChat, true, 1024)
	_, _ = io.ReadAll(reader)
	reader.Close()

	spans := sr.Ended()
	require.Len(t, spans, 1)

	hasCompletion := false
	for _, event := range spans[0].Events() {
		if event.Name == "gen_ai.content.completion" {
			hasCompletion = true
			assertAttribute(t, event.Attributes, "gen_ai.completion", "hello world")
		}
	}
	assert.True(t, hasCompletion, "missing completion event")
}

func TestStreamingReader_ContentCaptureDisabled(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tr := tp.Tracer("test")
	_, span := tr.Start(t.Context(), "test-content-disabled")

	streamData := "data: {\"id\":\"inc\",\"model\":\"gpt-4\",\"choices\":[{\"delta\":{\"content\":\"hello \"},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n"
	body := io.NopCloser(bytes.NewReader([]byte(streamData)))
	reader := NewStreamingReader(body, span, time.Now(), OpChat, false, 1024)
	_, _ = io.ReadAll(reader)
	reader.Close()

	spans := sr.Ended()
	require.Len(t, spans, 1)

	for _, event := range spans[0].Events() {
		if event.Name == "gen_ai.content.completion" {
			t.Fatalf("Content capture event emitted when disabled")
		}
	}
}

func TestStreamingReader_MemoryBoundsRespected(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tr := tp.Tracer("test")
	_, span := tr.Start(t.Context(), "test-bounds")

	streamData := "data: {\"id\":\"inc\",\"model\":\"gpt-4\",\"choices\":[{\"delta\":{\"content\":\"1234567890123456789012345678901234567890\"},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n"
	body := io.NopCloser(bytes.NewReader([]byte(streamData)))
	reader := NewStreamingReader(body, span, time.Now(), OpChat, true, 32)
	_, _ = io.ReadAll(reader)
	reader.Close()

	spans := sr.Ended()
	require.Len(t, spans, 1)

	hasCompletion := false
	for _, event := range spans[0].Events() {
		if event.Name == "gen_ai.content.completion" {
			hasCompletion = true
			assertAttribute(t, event.Attributes, "gen_ai.completion", "12345678901234567... [truncated]")
		}
	}
	assert.True(t, hasCompletion, "missing completion event")
}

func TestTruncateContent(t *testing.T) {
	content := strings.Repeat("a", ContentCaptureLimit+1)
	truncated := TruncateContent(content)

	assert.Len(t, truncated, ContentCaptureLimit)
	assert.True(t, strings.HasSuffix(truncated, truncatedContentSuffix))
}

func TestTruncateContentPreservesUTF8(t *testing.T) {
	content := strings.Repeat("é", ContentCaptureLimit)
	truncated := TruncateContent(content)

	assert.LessOrEqual(t, len(truncated), ContentCaptureLimit)
	assert.True(t, strings.HasSuffix(truncated, truncatedContentSuffix))
	assert.True(t, utf8.ValidString(truncated))
}

func TestStreamingReader_CompletionContentCapture(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	_, span := tp.Tracer("test").Start(t.Context(), "completion-content")

	streamData := "data: {\"choices\":[{\"index\":0,\"text\":\"hello \"}]}\n\n" +
		"data: {\"choices\":[{\"index\":0,\"text\":\"world\",\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"
	reader := NewStreamingReader(io.NopCloser(strings.NewReader(streamData)), span, time.Now(), OpCompletion, true, ContentCaptureLimit)
	_, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())

	spans := sr.Ended()
	require.Len(t, spans, 1)
	require.Len(t, spans[0].Events(), 1)
	assertAttribute(t, spans[0].Events()[0].Attributes, "gen_ai.completion", "hello world")
}

func TestStreamingReader_SeparatesChoiceContent(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	_, span := tp.Tracer("test").Start(t.Context(), "multiple-choices")

	streamData := "data: {\"choices\":[{\"index\":1,\"delta\":{\"content\":\"beta\"}},{\"index\":0,\"delta\":{\"content\":\"alpha\"}}]}\n\ndata: [DONE]\n\n"
	reader := NewStreamingReader(io.NopCloser(strings.NewReader(streamData)), span, time.Now(), OpChat, true, ContentCaptureLimit)
	_, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())

	spans := sr.Ended()
	require.Len(t, spans, 1)
	require.Len(t, spans[0].Events(), 2)
	assertAttribute(t, spans[0].Events()[0].Attributes, "gen_ai.completion", "alpha")
	assertAttribute(t, spans[0].Events()[1].Attributes, "gen_ai.completion", "beta")
}

func TestStreamingReader_AbortedStreamRecordsError(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	_, span := tp.Tracer("test").Start(t.Context(), "aborted-stream")

	streamData := "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"
	reader := NewStreamingReader(io.NopCloser(strings.NewReader(streamData)), span, time.Now(), OpChat, false, ContentCaptureLimit)
	_, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status().Code)
	assertSliceAttribute(t, spans[0].Attributes(), "gen_ai.response.finish_reasons", []string{"error"})
}
