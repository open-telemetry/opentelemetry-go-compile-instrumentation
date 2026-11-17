// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package genai

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// geminiStream is a streamGenerateContent response as the API sends it with
// alt=sse: one GenerateContentResponse per event, CRLF line endings, usage as
// running totals and the finish reason on the last event only.
const geminiStream = "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Hel\"}],\"role\":\"model\"}}]," +
	"\"usageMetadata\":{\"promptTokenCount\":11,\"candidatesTokenCount\":2,\"totalTokenCount\":13}," +
	"\"modelVersion\":\"gemini-2.5-flash-001\",\"responseId\":\"resp-stream-1\"}\r\n\r\n" +
	"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"lo\"}],\"role\":\"model\"}," +
	"\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":11,\"candidatesTokenCount\":5," +
	"\"thoughtsTokenCount\":3,\"totalTokenCount\":19},\"modelVersion\":\"gemini-2.5-flash-001\"," +
	"\"responseId\":\"resp-stream-1\"}\r\n\r\n"

// streamThrough drives a streamGenerateContent call against handler and
// returns the recorder plus the response, whose body the test then reads.
func streamThrough(t *testing.T, handler http.HandlerFunc) (*tracetest.SpanRecorder, *http.Response) {
	t.Helper()

	recorder := newRecordingTracer(t)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := &http.Client{Transport: &otelTransport{provider: providerGemini, system: systemGemini}}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		server.URL+"/v1beta/models/gemini-2.5-flash:streamGenerateContent?alt=sse",
		strings.NewReader(`{"contents":[],"generationConfig":{"maxOutputTokens":64}}`))
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)

	return recorder, resp
}

func serveStream(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}
}

// newTestStreamReader wraps body in a streamReader over a recorded span, the
// way RoundTrip hands a streaming response to it.
func newTestStreamReader(t *testing.T, body io.Reader) (*streamReader, *tracetest.SpanRecorder) {
	t.Helper()

	recorder := newRecordingTracer(t)
	// The reader under test owns the span and ends it when the stream finishes,
	// which is the behaviour these tests check.
	_, span := tracer.Start(t.Context(), "chat gemini-2.5-flash") //nolint:spancheck // ended by streamReader
	return newStreamReader(io.NopCloser(body), span), recorder    //nolint:spancheck // ended by streamReader
}

func singleSpan(t *testing.T, recorder *tracetest.SpanRecorder) sdktrace.ReadOnlySpan {
	t.Helper()

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	return spans[0]
}

func TestStream_SpanCoversWholeStream(t *testing.T) {
	recorder, resp := streamThrough(t, serveStream(geminiStream))

	// The span stays open while the caller is still reading.
	assert.Empty(t, recorder.Ended())

	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	// Every byte reaches the SDK unchanged.
	assert.Equal(t, geminiStream, string(got))

	span := singleSpan(t, recorder)
	assert.Equal(t, "chat gemini-2.5-flash", span.Name())
	assert.Equal(t, trace.SpanKindClient, span.SpanKind())
	assert.Equal(t, codes.Unset, span.Status().Code)

	attrs := attrsOf(t, span)
	assert.Equal(t, "gemini", attrs["gen_ai.system"].AsString())
	assert.Equal(t, "gcp.gemini", attrs["gen_ai.provider.name"].AsString())
	assert.Equal(t, "chat", attrs["gen_ai.operation.name"].AsString())
	assert.Equal(t, "gemini-2.5-flash", attrs["gen_ai.request.model"].AsString())
	assert.Equal(t, int64(64), attrs["gen_ai.request.max_tokens"].AsInt64())
	assert.True(t, attrs["gen_ai.request.is_stream"].AsBool())
	assert.Equal(t, "resp-stream-1", attrs["gen_ai.response.id"].AsString())
	assert.Equal(t, "gemini-2.5-flash-001", attrs["gen_ai.response.model"].AsString())
	assert.Equal(t, []string{"STOP"}, attrs["gen_ai.response.finish_reasons"].AsStringSlice())
	// Usage comes from the last event, not a sum of the running totals, and
	// thinking tokens are folded into output as on the non-streaming path.
	assert.Equal(t, int64(11), attrs["gen_ai.usage.input_tokens"].AsInt64())
	assert.Equal(t, int64(8), attrs["gen_ai.usage.output_tokens"].AsInt64())
	assert.Equal(t, int64(19), attrs["gen_ai.usage.total_tokens"].AsInt64())
}

func TestStream_ByteAtATimeReads(t *testing.T) {
	// Events split across reads must still be parsed whole.

	reader, recorder := newTestStreamReader(t, iotest.OneByteReader(strings.NewReader(geminiStream)))
	got, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, geminiStream, string(got))

	attrs := attrsOf(t, singleSpan(t, recorder))
	assert.Equal(t, []string{"STOP"}, attrs["gen_ai.response.finish_reasons"].AsStringSlice())
	assert.Equal(t, int64(19), attrs["gen_ai.usage.total_tokens"].AsInt64())
}

func TestStream_LastEventWithoutTrailingNewline(t *testing.T) {
	body := strings.TrimRight(geminiStream, "\r\n")
	recorder, resp := streamThrough(t, serveStream(body))

	_, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	attrs := attrsOf(t, singleSpan(t, recorder))
	assert.Equal(t, []string{"STOP"}, attrs["gen_ai.response.finish_reasons"].AsStringSlice())
	assert.Equal(t, int64(19), attrs["gen_ai.usage.total_tokens"].AsInt64())
}

func TestStream_MultipleCandidates(t *testing.T) {
	// Each candidate reports its own finish reason, on whichever event ends
	// it. A candidate at index 0 omits the index field.
	body := "data: {\"candidates\":[{\"content\":{}},{\"index\":1,\"finishReason\":\"MAX_TOKENS\"}]}\n\n" +
		"data: {\"candidates\":[{\"finishReason\":\"STOP\"}]}\n\n"
	recorder, resp := streamThrough(t, serveStream(body))

	_, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	attrs := attrsOf(t, singleSpan(t, recorder))
	assert.Equal(t, []string{"MAX_TOKENS", "STOP"}, attrs["gen_ai.response.finish_reasons"].AsStringSlice())
}

func TestStream_ClosedBeforeFinishIsAborted(t *testing.T) {
	recorder, resp := streamThrough(t, serveStream(geminiStream))

	// Read the first event only, as a caller breaking out of the SDK's
	// iterator would, then close.
	first := strings.Index(geminiStream, "\r\n\r\n") + len("\r\n\r\n")
	_, err := io.ReadFull(resp.Body, make([]byte, first))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	span := singleSpan(t, recorder)
	assert.Equal(t, codes.Error, span.Status().Code)

	// What arrived before the close is still recorded.
	attrs := attrsOf(t, span)
	assert.Equal(t, "resp-stream-1", attrs["gen_ai.response.id"].AsString())
	assert.Equal(t, int64(13), attrs["gen_ai.usage.total_tokens"].AsInt64())
	assert.NotContains(t, attrs, attribute.Key("gen_ai.response.finish_reasons"))
}

func TestStream_ClosedAfterFinishIsNotAnError(t *testing.T) {
	// The finish reason has arrived but the caller closes before reading EOF.
	reader, recorder := newTestStreamReader(t, strings.NewReader(geminiStream))
	_, err := io.ReadFull(reader, make([]byte, len(geminiStream)))
	require.NoError(t, err)
	require.NoError(t, reader.Close())

	assert.Equal(t, codes.Unset, singleSpan(t, recorder).Status().Code)
}

func TestStream_ReadErrorRecorded(t *testing.T) {
	wantErr := errors.New("connection reset")
	body := io.MultiReader(strings.NewReader("data: {\"responseId\":\"resp-1\"}\n\ndata: {\"cand"),
		iotest.ErrReader(wantErr))
	reader, recorder := newTestStreamReader(t, body)

	_, err := io.ReadAll(reader)
	require.ErrorIs(t, err, wantErr)
	require.NoError(t, reader.Close())

	got := singleSpan(t, recorder)
	assert.Equal(t, codes.Error, got.Status().Code)
	require.Len(t, got.Events(), 1)
	assert.Equal(t, "exception", got.Events()[0].Name)

	attrs := attrsOf(t, got)
	assert.Equal(t, "resp-1", attrs["gen_ai.response.id"].AsString())
	assert.Contains(t, attrs, attribute.Key("error.type"))
}

func TestStream_HTTPErrorEndsSpanImmediately(t *testing.T) {
	recorder, resp := streamThrough(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "quota exceeded", http.StatusTooManyRequests)
	})
	defer resp.Body.Close()

	// No stream to wait for: the span ends inside RoundTrip.
	span := singleSpan(t, recorder)
	assert.Equal(t, codes.Error, span.Status().Code)
	assert.Equal(t, "429", attrsOf(t, span)["error.type"].AsString())
}

func TestStream_IgnoresNonDataLinesAndBadJSON(t *testing.T) {
	body := ": keep-alive\n" +
		"event: message\n" +
		"data: {not json}\n\n" +
		"data:{\"responseId\":\"resp-2\",\"candidates\":[{\"finishReason\":\"STOP\"}]}\n\n"
	recorder, resp := streamThrough(t, serveStream(body))

	_, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	span := singleSpan(t, recorder)
	assert.Equal(t, codes.Unset, span.Status().Code)
	attrs := attrsOf(t, span)
	assert.Equal(t, "resp-2", attrs["gen_ai.response.id"].AsString())
	assert.Equal(t, []string{"STOP"}, attrs["gen_ai.response.finish_reasons"].AsStringSlice())
}

func TestStream_OversizedEventSkipped(t *testing.T) {
	oversized := "data: {\"responseId\":\"too-big\",\"pad\":\"" + strings.Repeat("x", maxStreamEventSize) + "\"}\n\n"
	body := oversized + "data: {\"candidates\":[{\"finishReason\":\"STOP\"}]}\n\n"
	reader, recorder := newTestStreamReader(t, strings.NewReader(body))

	got, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Len(t, got, len(body), "an oversized event still reaches the SDK")

	attrs := attrsOf(t, singleSpan(t, recorder))
	assert.NotContains(t, attrs, attribute.Key("gen_ai.response.id"))
	assert.Equal(t, []string{"STOP"}, attrs["gen_ai.response.finish_reasons"].AsStringSlice())
}

func TestStream_EndsSpanOnce(t *testing.T) {
	reader, recorder := newTestStreamReader(t, strings.NewReader(geminiStream))
	_, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	require.NoError(t, reader.Close())

	assert.Len(t, recorder.Ended(), 1)
}
