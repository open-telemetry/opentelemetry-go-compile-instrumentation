// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package v1

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

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/github.com/openai/openai-go/semconv"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/runtime"
)

const (
	maxRequestBodySize  = 1 << 20 // 1 MB
	maxResponseBodySize = 4 << 20 // 4 MB
)

var providerMapping = map[string]string{
	"openai.com":         "openai",
	"azure.com":          "azure",
	"anthropic.com":      "anthropic",
	"dashscope.aliyuncs": "qwen",
	"volces.com":         "ark",
	"ark.cn":             "ark",
	"hunyuan":            "tencent",
	"tencentcloudapi":    "tencent",
	"googleapis.com":     "google",
	"generativelanguage": "google",
	"deepseek.com":       "deepseek",
	"moonshot":           "moonshot",
	"zhipuai.cn":         "zhipu",
	"bigmodel.cn":        "zhipu",
	"baidu.com":          "baidu",
	"minimax":            "minimax",
	"siliconflow":        "siliconflow",
	"together":           "together",
	"mistral":            "mistral",
	"groq.com":           "groq",
	"ollama":             "ollama",
	"localhost":          "local",
	"127.0.0.1":          "local",
}

func getProviderName(host string) string {
	for keyword, provider := range providerMapping {
		if strings.Contains(host, keyword) {
			return provider
		}
	}
	return "openai"
}

type operationType int

const (
	opChat operationType = iota
	opCompletion
	opEmbedding
	opUnknown
)

func classifyOperation(path string) operationType {
	if strings.HasSuffix(path, "chat/completions") {
		return opChat
	}
	if strings.HasSuffix(path, "completions") {
		return opCompletion
	}
	if strings.HasSuffix(path, "embeddings") {
		return opEmbedding
	}
	return opUnknown
}

func operationName(op operationType) string {
	switch op {
	case opChat:
		return "chat"
	case opCompletion:
		return "text_completion"
	case opEmbedding:
		return "embeddings"
	default:
		return ""
	}
}

// OtelMiddleware returns an HTTP middleware that creates spans for OpenAI API
// calls following GenAI semantic conventions.
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

		var model string
		var spanAttrs []attribute.KeyValue

		switch op {
		case opChat:
			model, spanAttrs = parseChatRequest(bodyBytes)
		case opCompletion:
			model, spanAttrs = parseCompletionRequest(bodyBytes)
		case opEmbedding:
			model, spanAttrs = parseEmbeddingRequest(bodyBytes)
		}

		spanName := opName + " " + model
		baseAttrs := []attribute.KeyValue{
			semconv.GenAISystem("openai"),
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
			resp.Body = newStreamingReader(resp.Body, span, start, model, opName, provider, op, ctx)
		} else {
			handleNonStreamingResponse(ctx, resp, span, start, op)
		}

		return resp, nil
	}
}

func handleNonStreamingResponse(_ context.Context, resp *http.Response, span trace.Span, _ time.Time, op operationType) {
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

	switch op {
	case opChat:
		parseChatResponse(bodyBytes, span)
	case opCompletion:
		parseCompletionResponse(bodyBytes, span)
	case opEmbedding:
		parseEmbeddingResponse(bodyBytes, span)
	}
}

func parseChatRequest(body []byte) (string, []attribute.KeyValue) {
	var req struct {
		Model            string   `json:"model"`
		MaxTokens        *int64   `json:"max_tokens,omitempty"`
		Temperature      *float64 `json:"temperature,omitempty"`
		TopP             *float64 `json:"top_p,omitempty"`
		FrequencyPenalty *float64 `json:"frequency_penalty,omitempty"`
		PresencePenalty  *float64 `json:"presence_penalty,omitempty"`
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
	if req.FrequencyPenalty != nil {
		attrs = append(attrs, semconv.GenAIRequestFrequencyPenalty(*req.FrequencyPenalty))
	}
	if req.PresencePenalty != nil {
		attrs = append(attrs, semconv.GenAIRequestPresencePenalty(*req.PresencePenalty))
	}
	return req.Model, attrs
}

func parseCompletionRequest(body []byte) (string, []attribute.KeyValue) {
	var req struct {
		Model       string   `json:"model"`
		MaxTokens   *int64   `json:"max_tokens,omitempty"`
		Temperature *float64 `json:"temperature,omitempty"`
		TopP        *float64 `json:"top_p,omitempty"`
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
	return req.Model, attrs
}

func parseEmbeddingRequest(body []byte) (string, []attribute.KeyValue) {
	var req struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return "", nil
	}
	return req.Model, nil
}

func parseChatResponse(body []byte, span trace.Span) {
	var resp struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
			TotalTokens      int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return
	}

	var reasons []string
	for _, c := range resp.Choices {
		if c.FinishReason != "" {
			reasons = append(reasons, c.FinishReason)
		}
	}

	span.SetAttributes(
		semconv.GenAIResponseID(resp.ID),
		semconv.GenAIResponseModel(resp.Model),
		semconv.GenAIResponseFinishReasons(reasons),
		semconv.GenAIUsageInputTokens(resp.Usage.PromptTokens),
		semconv.GenAIUsageOutputTokens(resp.Usage.CompletionTokens),
		semconv.GenAIUsageTotalTokens(resp.Usage.TotalTokens),
	)
}

func parseCompletionResponse(body []byte, span trace.Span) {
	var resp struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
			TotalTokens      int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return
	}

	var reasons []string
	for _, c := range resp.Choices {
		if c.FinishReason != "" {
			reasons = append(reasons, c.FinishReason)
		}
	}

	span.SetAttributes(
		semconv.GenAIResponseID(resp.ID),
		semconv.GenAIResponseModel(resp.Model),
		semconv.GenAIResponseFinishReasons(reasons),
		semconv.GenAIUsageInputTokens(resp.Usage.PromptTokens),
		semconv.GenAIUsageOutputTokens(resp.Usage.CompletionTokens),
		semconv.GenAIUsageTotalTokens(resp.Usage.TotalTokens),
	)
}

func parseEmbeddingResponse(body []byte, span trace.Span) {
	var resp struct {
		Model string `json:"model"`
		Usage struct {
			PromptTokens int64 `json:"prompt_tokens"`
			TotalTokens  int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return
	}

	span.SetAttributes(
		semconv.GenAIResponseModel(resp.Model),
		semconv.GenAIUsageInputTokens(resp.Usage.PromptTokens),
		semconv.GenAIUsageTotalTokens(resp.Usage.TotalTokens),
	)
}
