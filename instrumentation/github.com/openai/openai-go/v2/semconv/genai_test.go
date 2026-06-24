// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
)

func TestGenAISystem(t *testing.T) {
	kv := GenAISystem("openai")
	assert.Equal(t, attribute.Key("gen_ai.system"), kv.Key)
	assert.Equal(t, "openai", kv.Value.AsString())
}

func TestGenAIOperationName(t *testing.T) {
	kv := GenAIOperationName("chat")
	assert.Equal(t, attribute.Key("gen_ai.operation.name"), kv.Key)
	assert.Equal(t, "chat", kv.Value.AsString())
}

func TestGenAIRequestModel(t *testing.T) {
	kv := GenAIRequestModel("gpt-4")
	assert.Equal(t, attribute.Key("gen_ai.request.model"), kv.Key)
	assert.Equal(t, "gpt-4", kv.Value.AsString())
}

func TestGenAIUsageInputTokens(t *testing.T) {
	kv := GenAIUsageInputTokens(100)
	assert.Equal(t, attribute.Key("gen_ai.usage.input_tokens"), kv.Key)
	assert.Equal(t, int64(100), kv.Value.AsInt64())
}

func TestGenAIUsageOutputTokens(t *testing.T) {
	kv := GenAIUsageOutputTokens(50)
	assert.Equal(t, attribute.Key("gen_ai.usage.output_tokens"), kv.Key)
	assert.Equal(t, int64(50), kv.Value.AsInt64())
}

func TestGenAIResponseFinishReasons(t *testing.T) {
	kv := GenAIResponseFinishReasons([]string{"stop", "length"})
	assert.Equal(t, attribute.Key("gen_ai.response.finish_reasons"), kv.Key)
	assert.Equal(t, []string{"stop", "length"}, kv.Value.AsStringSlice())
}

func TestGenAIProviderName(t *testing.T) {
	kv := GenAIProviderName("openai")
	assert.Equal(t, attribute.Key("gen_ai.provider.name"), kv.Key)
	assert.Equal(t, "openai", kv.Value.AsString())
}

func TestGenAIRequestIsStream(t *testing.T) {
	kv := GenAIRequestIsStream(true)
	assert.Equal(t, attribute.Key("gen_ai.request.is_stream"), kv.Key)
	assert.True(t, kv.Value.AsBool())
}

func TestGenAIRequestFrequencyPenalty(t *testing.T) {
	kv := GenAIRequestFrequencyPenalty(0.5)
	assert.Equal(t, attribute.Key("gen_ai.request.frequency_penalty"), kv.Key)
	assert.Equal(t, 0.5, kv.Value.AsFloat64())
}

func TestGenAIRequestPresencePenalty(t *testing.T) {
	kv := GenAIRequestPresencePenalty(0.8)
	assert.Equal(t, attribute.Key("gen_ai.request.presence_penalty"), kv.Key)
	assert.Equal(t, 0.8, kv.Value.AsFloat64())
}
