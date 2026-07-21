// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otelc/instrumentation/github.com/anthropics/anthropic-sdk-go/semconv"
	"go.opentelemetry.io/otelc/pkg/runtime"
)

const (
	maxRequestBodySize  = 1 << 20 // 1 MB
	maxResponseBodySize = 4 << 20 // 4 MB
)

// providerEntries is an ordered slice so keyword matching has deterministic
// priority, unlike map iteration.
var providerEntries = []struct{ keyword, provider string }{
	{"anthropic.com", "anthropic"},
	{"localhost", "local"},
	{"127.0.0.1", "local"},
}

func getProviderName(host string) string {
	for _, e := range providerEntries {
		if strings.Contains(host, e.keyword) {
			return e.provider
		}
	}
	return "anthropic"
}

type operationType int

const (
	opMessages operationType = iota
	opUnknown
)

// classifyOperation maps a request path to an operation. Only the Messages API
// (POST /v1/messages) is instrumented; the suffix match excludes
// /v1/messages/count_tokens and /v1/messages/batches.
func classifyOperation(path string) operationType {
	if strings.HasSuffix(path, "/messages") {
		return opMessages
	}
	return opUnknown
}

func operationName(op operationType) string {
	if op == opMessages {
		return "chat"
	}
	return ""
}

// OtelMiddleware returns an HTTP middleware that creates spans for Anthropic
// API calls following GenAI semantic conventions.
func OtelMiddleware() func(*http.Request, func(*http.Request) (*http.Response, error)) (*http.Response, error) {
	return func(req *http.Request, next func(*http.Request) (*http.Response, error)) (*http.Response, error) {
		if req.Body == nil {
			return next(req)
		}

		op := classifyOperation(req.URL.Path)
		if op == opUnknown {
			return next(req)
		}

		start := time.Now()
		provider := getProviderName(req.URL.Host)
		opName := operationName(op)

		// Read a bounded copy for attribute parsing, but preserve the full body for the SDK.
		var buf bytes.Buffer
		tee := io.TeeReader(req.Body, &buf)
		bodyBytes, err := io.ReadAll(io.LimitReader(tee, maxRequestBodySize))
		// Reassemble regardless of the read outcome so the SDK always sees
		// the full body: buffered bytes + remaining unread body.
		req.Body = struct {
			io.Reader
			io.Closer
		}{io.MultiReader(&buf, req.Body), req.Body}
		if err != nil {
			return next(req)
		}

		model, isStream, spanAttrs := parseMessagesRequest(bodyBytes)

		// Streaming responses need event accumulation before their spans carry
		// usage data; until that lands (#679, follow-up PR), pass streaming
		// requests through uninstrumented rather than emit incomplete spans.
		if isStream {
			return next(req)
		}

		spanName := opName + " " + model
		baseAttrs := []attribute.KeyValue{
			semconv.GenAISystem("anthropic"),
			semconv.GenAIOperationName(opName),
			semconv.GenAIRequestModel(model),
			semconv.GenAIProviderName(provider),
		}
		spanAttrs = append(baseAttrs, spanAttrs...)

		ctx := req.Context()
		ctx, span := tracer.Start(ctx, spanName,
			trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(spanAttrs...),
		)
		ctx = runtime.SuppressHTTPClientInstrumentation(ctx)
		req = req.WithContext(ctx)

		resp, err := next(req)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err)
			span.End()
			return resp, err
		}

		if resp.StatusCode >= 400 {
			span.SetStatus(codes.Error, resp.Status)
			span.SetAttributes(attribute.String("error.type", resp.Status))
			span.End()
			return resp, nil
		}

		// Streaming requests were already passed through above; if the server
		// still answers with SSE, end the span without response attributes
		// rather than hold it open on a body we do not accumulate yet.
		contentType := resp.Header.Get("Content-Type")
		if strings.HasPrefix(contentType, "text/event-stream") {
			span.SetAttributes(semconv.GenAIRequestIsStream(true))
			span.End()
			return resp, nil
		}

		handleNonStreamingResponse(ctx, resp, span, start)

		return resp, nil
	}
}

func handleNonStreamingResponse(
	_ context.Context,
	resp *http.Response,
	span trace.Span,
	_ time.Time,
) {
	defer span.End()

	if resp.Body == nil {
		return
	}

	// Read a bounded preview for parsing, but reassemble the full body for
	// callers regardless of the read outcome.
	var buf bytes.Buffer
	tee := io.TeeReader(resp.Body, &buf)
	bodyBytes, err := io.ReadAll(io.LimitReader(tee, maxResponseBodySize))
	resp.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(&buf, resp.Body), resp.Body}
	if err != nil {
		return
	}

	parseMessagesResponse(bodyBytes, span)
}

func parseMessagesRequest(body []byte) (string, bool, []attribute.KeyValue) {
	var req struct {
		Model       string   `json:"model"`
		Stream      bool     `json:"stream,omitempty"`
		MaxTokens   *int64   `json:"max_tokens,omitempty"`
		Temperature *float64 `json:"temperature,omitempty"`
		TopP        *float64 `json:"top_p,omitempty"`
		TopK        *int64   `json:"top_k,omitempty"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return "", false, nil
	}

	var attrs []attribute.KeyValue
	if req.MaxTokens != nil {
		attrs = append(attrs, semconv.GenAIRequestMaxTokens(*req.MaxTokens))
	}
	if req.Temperature != nil {
		attrs = append(attrs, semconv.GenAIRequestTemperature(*req.Temperature))
	}
	if req.TopP != nil {
		attrs = append(attrs, semconv.GenAIRequestTopP(*req.TopP))
	}
	if req.TopK != nil {
		attrs = append(attrs, semconv.GenAIRequestTopK(*req.TopK))
	}
	return req.Model, req.Stream, attrs
}

func parseMessagesResponse(body []byte, span trace.Span) {
	var resp struct {
		ID         string `json:"id"`
		Model      string `json:"model"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens              int64 `json:"input_tokens"`
			OutputTokens             int64 `json:"output_tokens"`
			CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return
	}

	// Anthropic returns a single stop_reason rather than per-choice
	// finish_reason values; wrap it to keep the shared attribute shape.
	var reasons []string
	if resp.StopReason != "" {
		reasons = append(reasons, resp.StopReason)
	}

	// Unlike OpenAI's prompt_tokens, Anthropic's input_tokens excludes cache
	// reads and creations, which are reported separately. Fold them back in so
	// gen_ai.usage.input_tokens reflects the full prompt per semconv.
	totalInput := resp.Usage.InputTokens +
		resp.Usage.CacheReadInputTokens +
		resp.Usage.CacheCreationInputTokens

	span.SetAttributes(
		semconv.GenAIResponseID(resp.ID),
		semconv.GenAIResponseModel(resp.Model),
		semconv.GenAIResponseFinishReasons(reasons),
		semconv.GenAIUsageInputTokens(totalInput),
		semconv.GenAIUsageOutputTokens(resp.Usage.OutputTokens),
		// The Messages API reports no total_tokens field; derive it so the
		// span shape matches the other GenAI instrumentations.
		semconv.GenAIUsageTotalTokens(totalInput+resp.Usage.OutputTokens),
	)

	// Prompt-cache usage is Anthropic-specific; only record it when the
	// request actually used the cache.
	if resp.Usage.CacheReadInputTokens > 0 {
		span.SetAttributes(semconv.GenAIUsageCacheReadInputTokens(resp.Usage.CacheReadInputTokens))
	}
	if resp.Usage.CacheCreationInputTokens > 0 {
		span.SetAttributes(semconv.GenAIUsageCacheCreationInputTokens(resp.Usage.CacheCreationInputTokens))
	}
}
