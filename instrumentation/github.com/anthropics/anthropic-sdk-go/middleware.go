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

var providerMapping = map[string]string{
	"anthropic.com": "anthropic",
	"localhost":     "local",
	"127.0.0.1":     "local",
}

func getProviderName(host string) string {
	for keyword, provider := range providerMapping {
		if strings.Contains(host, keyword) {
			return provider
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
		if err != nil {
			return next(req)
		}
		// Reassemble: buffered bytes + remaining unread body.
		req.Body = struct {
			io.Reader
			io.Closer
		}{io.MultiReader(&buf, req.Body), req.Body}

		model, spanAttrs := parseMessagesRequest(bodyBytes)

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

		contentType := resp.Header.Get("Content-Type")
		isStreaming := strings.HasPrefix(contentType, "text/event-stream")

		if isStreaming {
			span.SetAttributes(semconv.GenAIRequestIsStream(true))
			resp.Body = newStreamingReader(resp.Body, span, start)
		} else {
			handleNonStreamingResponse(ctx, resp, span, start)
		}

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

	// Read a bounded preview for parsing, but reassemble the full body for callers.
	var buf bytes.Buffer
	tee := io.TeeReader(resp.Body, &buf)
	bodyBytes, err := io.ReadAll(io.LimitReader(tee, maxResponseBodySize))
	if err != nil {
		return
	}
	// Reassemble: preview bytes + remaining unread body.
	resp.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(&buf, resp.Body), resp.Body}

	parseMessagesResponse(bodyBytes, span)
}

func parseMessagesRequest(body []byte) (string, []attribute.KeyValue) {
	var req struct {
		Model       string   `json:"model"`
		MaxTokens   *int64   `json:"max_tokens,omitempty"`
		Temperature *float64 `json:"temperature,omitempty"`
		TopP        *float64 `json:"top_p,omitempty"`
		TopK        *int64   `json:"top_k,omitempty"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return "", nil
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
	return req.Model, attrs
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

	span.SetAttributes(
		semconv.GenAIResponseID(resp.ID),
		semconv.GenAIResponseModel(resp.Model),
		semconv.GenAIResponseFinishReasons(reasons),
		semconv.GenAIUsageInputTokens(resp.Usage.InputTokens),
		semconv.GenAIUsageOutputTokens(resp.Usage.OutputTokens),
		// The Messages API reports no total_tokens field; derive it so the
		// span shape matches the other GenAI instrumentations.
		semconv.GenAIUsageTotalTokens(resp.Usage.InputTokens+resp.Usage.OutputTokens),
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
