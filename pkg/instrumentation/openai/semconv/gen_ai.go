// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"go.opentelemetry.io/otel/attribute"
)

// GenAI semantic convention attribute keys.
// See: https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-spans/
var (
	GenAISystemKey                = attribute.Key("gen_ai.system")
	GenAIOperationNameKey         = attribute.Key("gen_ai.operation.name")
	GenAIRequestModelKey          = attribute.Key("gen_ai.request.model")
	GenAIResponseIDKey            = attribute.Key("gen_ai.response.id")
	GenAIResponseModelKey         = attribute.Key("gen_ai.response.model")
	GenAIResponseFinishReasonsKey = attribute.Key("gen_ai.response.finish_reasons")
	GenAIUsageInputTokensKey      = attribute.Key("gen_ai.usage.input_tokens")
	GenAIUsageOutputTokensKey     = attribute.Key("gen_ai.usage.output_tokens")
)

// GenAI system value for OpenAI.
const GenAISystemOpenAI = "openai"

// GenAI operation names.
const (
	OperationChat = "chat"
)

// RequestTraceAttrs returns common request attributes for any GenAI operation.
func RequestTraceAttrs(operation, model string) []attribute.KeyValue {
	return []attribute.KeyValue{
		GenAISystemKey.String(GenAISystemOpenAI),
		GenAIOperationNameKey.String(operation),
		GenAIRequestModelKey.String(model),
	}
}

// ChatCompletionResponseTraceAttrs returns response attributes for chat completions.
func ChatCompletionResponseTraceAttrs(
	id, model string,
	finishReasons []string,
	inputTokens, outputTokens int64,
) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		GenAIResponseIDKey.String(id),
		GenAIResponseModelKey.String(model),
		GenAIUsageInputTokensKey.Int64(inputTokens),
		GenAIUsageOutputTokensKey.Int64(outputTokens),
	}
	if len(finishReasons) > 0 {
		attrs = append(attrs, GenAIResponseFinishReasonsKey.StringSlice(finishReasons))
	}
	return attrs
}
