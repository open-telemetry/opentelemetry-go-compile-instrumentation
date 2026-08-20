// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package v2

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	otelsemconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otelc/instrumentation/github.com/openai/openai-go/internal/streaming"
	"go.opentelemetry.io/otelc/instrumentation/github.com/openai/openai-go/v2/semconv"
	"go.opentelemetry.io/otelc/pkg/runtime"
)

var captureContentEnabled = sync.OnceValue(func() bool {
	return os.Getenv("OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT") == "true"
})

const (
	maxRequestBodySize  = 1 << 20 // 1 MB
	maxResponseBodySize = 4 << 20 // 4 MB
)

// providerMapping is checked in order: the first entry whose keyword is a
// substring of the request host wins. It is a slice, not a map, because Go
// map iteration order is randomized on every range - a map here would make
// getProviderName non-deterministic whenever a host matched more than one
// keyword (see Issue #824).
var providerMapping = []struct { //nolint:gochecknoglobals // private lookup table
	keyword  string
	provider string
}{
	{"openai.com", "openai"},
	{"azure.com", "azure"},
	{"anthropic.com", "anthropic"},
	{"dashscope.aliyuncs", "qwen"},
	{"volces.com", "ark"},
	{"ark.cn", "ark"},
	{"hunyuan", "tencent"},
	{"tencentcloudapi", "tencent"},
	{"googleapis.com", "google"},
	{"generativelanguage", "google"},
	{"deepseek.com", "deepseek"},
	{"moonshot", "moonshot"},
	{"zhipuai.cn", "zhipu"},
	{"bigmodel.cn", "zhipu"},
	{"baidu.com", "baidu"},
	{"minimax", "minimax"},
	{"siliconflow", "siliconflow"},
	{"together", "together"},
	{"mistral", "mistral"},
	{"groq.com", "groq"},
	{"ollama", "ollama"},
	{"localhost", "local"},
	{"127.0.0.1", "local"},
}

func getProviderName(host string) string {
	for _, entry := range providerMapping {
		if strings.Contains(host, entry.keyword) {
			return entry.provider
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
	return otelMiddleware(captureContentEnabled)
}

func otelMiddleware(
	isContentCaptureEnabled func() bool,
) func(*http.Request, func(*http.Request) (*http.Response, error)) (*http.Response, error) {
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
		// Reassemble: buffered bytes + remaining unread body.
		req.Body = struct {
			io.Reader
			io.Closer
		}{io.MultiReader(&buf, req.Body), req.Body}
		if err != nil {
			return next(req)
		}

		var model string
		var spanAttrs []attribute.KeyValue
		captureContent := isContentCaptureEnabled()
		var prompts []string

		switch op {
		case opChat:
			model, spanAttrs, prompts = parseChatRequest(bodyBytes, captureContent)
		case opCompletion:
			model, spanAttrs, prompts = parseCompletionRequest(bodyBytes, captureContent)
		case opEmbedding:
			model, spanAttrs = parseEmbeddingRequest(bodyBytes)
		}

		if model == "" {
			return next(req)
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

		if captureContent {
			recordContentEvents(span, "gen_ai.content.prompt", "gen_ai.prompt", prompts)
		}
		ctx = runtime.SuppressHTTPClientInstrumentation(ctx)
		req = req.WithContext(ctx)

		resp, err := next(req)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err)
			span.SetAttributes(otelsemconv.ErrorType(err))
			span.End()
			return resp, err
		}

		if resp.StatusCode >= 400 {
			span.RecordError(errors.New(resp.Status))
			span.SetStatus(codes.Error, resp.Status)
			span.SetAttributes(otelsemconv.ErrorTypeKey.String(strconv.Itoa(resp.StatusCode)))
			span.End()
			return resp, nil
		}

		contentType := resp.Header.Get("Content-Type")
		isStreaming := strings.HasPrefix(contentType, "text/event-stream")

		if isStreaming {
			span.SetAttributes(semconv.GenAIRequestIsStream(true))
			resp.Body = newStreamingReader(resp.Body, span, start, op, captureContent)
		} else {
			handleNonStreamingResponse(ctx, resp, span, start, op, captureContent)
		}

		return resp, nil
	}
}

func handleNonStreamingResponse(
	_ context.Context,
	resp *http.Response,
	span trace.Span,
	_ time.Time,
	op operationType,
	captureContent bool,
) {
	defer span.End()

	// Read a bounded preview for parsing, but reassemble the full body for callers.
	var buf bytes.Buffer
	tee := io.TeeReader(resp.Body, &buf)
	bodyBytes, err := io.ReadAll(io.LimitReader(tee, maxResponseBodySize))
	// Reassemble: preview bytes + remaining unread body.
	resp.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(&buf, resp.Body), resp.Body}
	if err != nil {
		return
	}

	switch op {
	case opChat:
		parseChatResponse(bodyBytes, span, captureContent)
	case opCompletion:
		parseCompletionResponse(bodyBytes, span, captureContent)
	case opEmbedding:
		parseEmbeddingResponse(bodyBytes, span)
	}
}

func parseChatRequest(body []byte, captureContent bool) (string, []attribute.KeyValue, []string) {
	var req struct {
		Model            string   `json:"model"`
		MaxTokens        *int64   `json:"max_tokens,omitempty"`
		Temperature      *float64 `json:"temperature,omitempty"`
		TopP             *float64 `json:"top_p,omitempty"`
		FrequencyPenalty *float64 `json:"frequency_penalty,omitempty"`
		PresencePenalty  *float64 `json:"presence_penalty,omitempty"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return "", nil, nil
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
	if !captureContent {
		return req.Model, attrs, nil
	}
	return req.Model, attrs, parseChatPrompts(body)
}

func parseCompletionRequest(body []byte, captureContent bool) (string, []attribute.KeyValue, []string) {
	var req struct {
		Model       string   `json:"model"`
		MaxTokens   *int64   `json:"max_tokens,omitempty"`
		Temperature *float64 `json:"temperature,omitempty"`
		TopP        *float64 `json:"top_p,omitempty"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return "", nil, nil
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
	if !captureContent {
		return req.Model, attrs, nil
	}
	return req.Model, attrs, parseCompletionPrompts(body)
}

func parseChatPrompts(body []byte) []string {
	var req struct {
		Messages []map[string]json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil
	}

	prompts := make([]string, 0, len(req.Messages))
	for _, message := range req.Messages {
		if content := contentFromJSON(message["content"]); content != "" {
			prompts = append(prompts, content)
		}
	}
	return prompts
}

func parseCompletionPrompts(body []byte) []string {
	var req struct {
		Prompt json.RawMessage `json:"prompt"`
	}
	if err := json.Unmarshal(body, &req); err != nil || len(req.Prompt) == 0 {
		return nil
	}

	var prompt string
	if err := json.Unmarshal(req.Prompt, &prompt); err == nil {
		return []string{prompt}
	}

	var prompts []string
	if err := json.Unmarshal(req.Prompt, &prompts); err != nil {
		return nil
	}
	return prompts
}

func contentFromJSON(content json.RawMessage) string {
	content = bytes.TrimSpace(content)
	if len(content) == 0 || bytes.Equal(content, []byte("null")) {
		return ""
	}

	var text string
	if err := json.Unmarshal(content, &text); err == nil {
		return text
	}

	marshaled, err := json.Marshal(content)
	if err != nil {
		return ""
	}
	return string(marshaled)
}

func recordContentEvents(span trace.Span, name, key string, contents []string) {
	for _, content := range contents {
		if content == "" {
			continue
		}
		span.AddEvent(name, trace.WithAttributes(
			attribute.String(key, streaming.TruncateContent(content)),
		))
	}
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

func parseChatResponse(body []byte, span trace.Span, captureContent bool) {
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
	if captureContent {
		recordContentEvents(span, "gen_ai.content.completion", "gen_ai.completion", parseChatCompletions(body))
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

func parseCompletionResponse(body []byte, span trace.Span, captureContent bool) {
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
	if captureContent {
		recordContentEvents(span, "gen_ai.content.completion", "gen_ai.completion", parseCompletionContents(body))
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

func parseChatCompletions(body []byte) []string {
	var resp struct {
		Choices []struct {
			Message map[string]json.RawMessage `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil
	}

	completions := make([]string, 0, len(resp.Choices))
	for _, choice := range resp.Choices {
		if content := contentFromJSON(choice.Message["content"]); content != "" {
			completions = append(completions, content)
		}
	}
	return completions
}

func parseCompletionContents(body []byte) []string {
	var resp struct {
		Choices []struct {
			Text string `json:"text"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil
	}

	completions := make([]string, 0, len(resp.Choices))
	for _, choice := range resp.Choices {
		if choice.Text != "" {
			completions = append(completions, choice.Text)
		}
	}
	return completions
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
