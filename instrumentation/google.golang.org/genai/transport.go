// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package genai

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	otelsemconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otelc/instrumentation/google.golang.org/genai/semconv"
	"go.opentelemetry.io/otelc/pkg/netutil"
	"go.opentelemetry.io/otelc/pkg/runtime"
)

const (
	maxRequestBodySize  = 1 << 20 // 1 MB
	maxResponseBodySize = 4 << 20 // 4 MB

	operationChat = "chat"
)

// responseTruncatedWarnOnce logs the size-cap warning once per process rather
// than once per oversized response.
var responseTruncatedWarnOnce sync.Once

// otelTransport wraps the round tripper the GenAI SDK issues requests through
// and emits one client span per non-streaming generateContent call.
//
// Message content (prompts and completions) is never read onto the span. The
// GenAI semantic conventions gate content capture behind
// OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT, which this
// instrumentation does not implement yet, so the privacy-safe default is the
// only behaviour: metadata only.
type otelTransport struct {
	base     http.RoundTripper
	provider string
	system   string
}

func (t *otelTransport) roundTripper() http.RoundTripper {
	if t.base != nil {
		return t.base
	}
	return http.DefaultTransport
}

type operationType int

const (
	opChat operationType = iota
	opStreamChat
	opUnknown
)

// classifyOperation maps a Gemini REST path onto an operation and the model it
// targets. Both backends end the path with "<model>:<rpc>":
//
//	/v1beta/models/gemini-2.5-flash:generateContent
//	/v1beta1/projects/p/locations/l/publishers/google/models/gemini-2.5-flash:generateContent
//
// Only the final segment is needed: the part before ":" is the model id that
// gen_ai.request.model reports, and the part after it names the RPC.
func classifyOperation(path string) (operationType, string) {
	segment := path[strings.LastIndex(path, "/")+1:]
	model, rpc, found := strings.Cut(segment, ":")
	if !found || model == "" {
		return opUnknown, ""
	}

	switch rpc {
	case "generateContent":
		return opChat, model
	case "streamGenerateContent":
		return opStreamChat, model
	default:
		// countTokens, embedContent, predict and the rest are out of scope.
		return opUnknown, ""
	}
}

func (t *otelTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	op, model := classifyOperation(req.URL.Path)
	if op == opUnknown {
		return t.roundTripper().RoundTrip(req)
	}

	// Streaming responses only report usage in their final server-sent event,
	// so a span for them needs SSE accumulation. Until that lands, pass
	// streaming calls through uninstrumented rather than emit a span that is
	// missing every response attribute.
	if op == opStreamChat {
		return t.roundTripper().RoundTrip(req)
	}

	attrs := []attribute.KeyValue{
		semconv.GenAISystem(t.system),
		otelsemconv.GenAIProviderNameKey.String(t.provider),
		otelsemconv.GenAIOperationNameChat,
		otelsemconv.GenAIRequestModel(model),
	}
	if address, port := netutil.HTTPServerEndpoint(req.URL); address != "" && port > 0 {
		attrs = append(attrs, otelsemconv.ServerAddress(address), otelsemconv.ServerPort(port))
	}
	attrs = append(attrs, requestConfigAttrs(req)...)

	ctx, span := tracer.Start(req.Context(), operationChat+" "+model,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrs...),
	)
	defer span.End()

	// otelc also instruments net/http's own RoundTrip, one layer below this
	// one. Suppress it so a single GenAI call produces a single span.
	ctx = runtime.SuppressHTTPClientInstrumentation(ctx)

	// RoundTrippers must not mutate the request they are given; WithContext
	// returns the required shallow copy.
	resp, err := t.roundTripper().RoundTrip(req.WithContext(ctx))
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		span.SetAttributes(otelsemconv.ErrorType(err))
		return resp, err
	}

	if resp.StatusCode >= http.StatusBadRequest {
		span.RecordError(errors.New(resp.Status))
		span.SetStatus(codes.Error, resp.Status)
		span.SetAttributes(otelsemconv.ErrorTypeKey.String(strconv.Itoa(resp.StatusCode)))
		return resp, nil
	}

	// Defensive only: the SDK sends generateContent without alt=sse and asks
	// for SSE solely on streamGenerateContent, which returned above, so a
	// conforming server never takes this branch. A proxy or test double that
	// answers with SSE anyway gets a span without response attributes rather
	// than a blocked read on a body this instrumentation does not accumulate.
	// The Anthropic instrumentation keeps the same guard.
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		span.SetAttributes(semconv.GenAIRequestIsStream(true))
		return resp, nil
	}

	recordResponse(resp, span)

	return resp, nil
}

// requestConfigAttrs reports the generation parameters carried in the request
// body. It reads through GetBody, which net/http populates because the SDK
// builds the request over a *bytes.Buffer, so the body the server receives is
// never consumed or reassembled. These attributes are recommended rather than
// required, so any body that cannot be re-read is simply skipped.
func requestConfigAttrs(req *http.Request) []attribute.KeyValue {
	if req.GetBody == nil {
		return nil
	}
	body, err := req.GetBody()
	if err != nil || body == nil {
		return nil
	}
	defer body.Close()

	raw, err := io.ReadAll(io.LimitReader(body, maxRequestBodySize))
	if err != nil {
		return nil
	}

	var parsed struct {
		GenerationConfig struct {
			Temperature     *float64 `json:"temperature"`
			TopP            *float64 `json:"topP"`
			TopK            *float64 `json:"topK"`
			MaxOutputTokens *int64   `json:"maxOutputTokens"`
		} `json:"generationConfig"`
	}
	if err = json.Unmarshal(raw, &parsed); err != nil {
		return nil
	}

	config := parsed.GenerationConfig
	var attrs []attribute.KeyValue
	if config.MaxOutputTokens != nil {
		attrs = append(attrs, otelsemconv.GenAIRequestMaxTokensKey.Int64(*config.MaxOutputTokens))
	}
	if config.Temperature != nil {
		attrs = append(attrs, otelsemconv.GenAIRequestTemperature(*config.Temperature))
	}
	if config.TopP != nil {
		attrs = append(attrs, otelsemconv.GenAIRequestTopP(*config.TopP))
	}
	if config.TopK != nil {
		// gen_ai.request.top_k is a double in semconv, matching the API's
		// float topK, so it is reported without conversion.
		attrs = append(attrs, otelsemconv.GenAIRequestTopK(*config.TopK))
	}
	return attrs
}

// readBounded reads up to limit bytes from body for attribute parsing while
// preserving the full stream for the SDK. truncated reports whether body held
// more than limit bytes; in that case bodyBytes is nil so no caller can parse a
// cut-off prefix as JSON.
func readBounded(body io.ReadCloser, limit int64) (bodyBytes []byte, truncated bool, reassembled io.ReadCloser, err error) {
	var buf bytes.Buffer
	tee := io.TeeReader(body, &buf)
	// Read one byte past the limit so a body of exactly limit bytes can be
	// told apart from a longer one. The tee captures that byte too, so the
	// reassembled stream is complete either way.
	read, err := io.ReadAll(io.LimitReader(tee, limit+1))
	reassembled = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(&buf, body), body}
	if int64(len(read)) > limit {
		return nil, true, reassembled, err
	}
	return read, false, reassembled, err
}

// recordResponse parses the generateContent response onto the span. It reads a
// bounded preview and hands the full body back to the SDK: the buffered bytes
// followed by whatever is still unread.
func recordResponse(resp *http.Response, span trace.Span) {
	if resp.Body == nil {
		return
	}

	raw, truncated, body, err := readBounded(resp.Body, maxResponseBodySize)
	resp.Body = body
	if err != nil {
		return
	}
	if truncated {
		responseTruncatedWarnOnce.Do(func() {
			logger.Warn("Gemini response body exceeds the size cap; response attributes omitted",
				"limit_bytes", maxResponseBodySize)
		})
		return
	}

	span.SetAttributes(responseAttrs(raw)...)
}

func responseAttrs(body []byte) []attribute.KeyValue {
	var parsed struct {
		ResponseID   string `json:"responseId"`
		ModelVersion string `json:"modelVersion"`
		Candidates   []struct {
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		UsageMetadata *struct {
			PromptTokenCount     int64 `json:"promptTokenCount"`
			CandidatesTokenCount int64 `json:"candidatesTokenCount"`
			ThoughtsTokenCount   int64 `json:"thoughtsTokenCount"`
			TotalTokenCount      int64 `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil
	}

	var attrs []attribute.KeyValue
	if parsed.ResponseID != "" {
		attrs = append(attrs, otelsemconv.GenAIResponseID(parsed.ResponseID))
	}
	if parsed.ModelVersion != "" {
		attrs = append(attrs, otelsemconv.GenAIResponseModel(parsed.ModelVersion))
	}

	reasons := make([]string, 0, len(parsed.Candidates))
	for _, candidate := range parsed.Candidates {
		if candidate.FinishReason != "" {
			reasons = append(reasons, candidate.FinishReason)
		}
	}
	if len(reasons) > 0 {
		attrs = append(attrs, otelsemconv.GenAIResponseFinishReasons(reasons...))
	}

	usage := parsed.UsageMetadata
	if usage == nil {
		return attrs
	}

	// Thinking tokens are billed and generated as output but reported apart
	// from candidatesTokenCount, so fold them in: gen_ai.usage.output_tokens is
	// defined as the tokens the model produced.
	outputTokens := usage.CandidatesTokenCount + usage.ThoughtsTokenCount

	// promptTokenCount is already the full effective prompt, cached content
	// included, so it maps onto gen_ai.usage.input_tokens as-is.
	totalTokens := usage.TotalTokenCount
	if totalTokens == 0 {
		// Not reported (or an all-zero usage block); derive it so the span
		// shape matches the other GenAI instrumentations.
		totalTokens = usage.PromptTokenCount + outputTokens
	}

	return append(attrs,
		otelsemconv.GenAIUsageInputTokensKey.Int64(usage.PromptTokenCount),
		otelsemconv.GenAIUsageOutputTokensKey.Int64(outputTokens),
		semconv.GenAIUsageTotalTokens(totalTokens),
	)
}
