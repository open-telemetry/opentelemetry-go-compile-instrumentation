// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
)

func TestGenAISystem(t *testing.T) {
	kv := GenAISystem("anthropic")
	assert.Equal(t, attribute.Key("gen_ai.system"), kv.Key)
	assert.Equal(t, "anthropic", kv.Value.AsString())
}

func TestGenAIOperationName(t *testing.T) {
	kv := GenAIOperationName("chat")
	assert.Equal(t, attribute.Key("gen_ai.operation.name"), kv.Key)
	assert.Equal(t, "chat", kv.Value.AsString())
}

func TestGenAIRequestModel(t *testing.T) {
	kv := GenAIRequestModel("claude-3-5-sonnet")
	assert.Equal(t, attribute.Key("gen_ai.request.model"), kv.Key)
	assert.Equal(t, "claude-3-5-sonnet", kv.Value.AsString())
}

func TestGenAIResponseModel(t *testing.T) {
	kv := GenAIResponseModel("claude-3-5-sonnet-20241022")
	assert.Equal(t, attribute.Key("gen_ai.response.model"), kv.Key)
	assert.Equal(t, "claude-3-5-sonnet-20241022", kv.Value.AsString())
}

func TestGenAIResponseID(t *testing.T) {
	kv := GenAIResponseID("msg_abc123")
	assert.Equal(t, attribute.Key("gen_ai.response.id"), kv.Key)
	assert.Equal(t, "msg_abc123", kv.Value.AsString())
}

func TestGenAIResponseFinishReasons(t *testing.T) {
	kv := GenAIResponseFinishReasons([]string{"end_turn", "max_tokens"})
	assert.Equal(t, attribute.Key("gen_ai.response.finish_reasons"), kv.Key)
	assert.Equal(t, []string{"end_turn", "max_tokens"}, kv.Value.AsStringSlice())
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

func TestGenAIUsageTotalTokens(t *testing.T) {
	kv := GenAIUsageTotalTokens(150)
	assert.Equal(t, attribute.Key("gen_ai.usage.total_tokens"), kv.Key)
	assert.Equal(t, int64(150), kv.Value.AsInt64())
}

func TestGenAIUsageCacheReadInputTokens(t *testing.T) {
	kv := GenAIUsageCacheReadInputTokens(64)
	assert.Equal(t, attribute.Key("gen_ai.usage.cache_read.input_tokens"), kv.Key)
	assert.Equal(t, int64(64), kv.Value.AsInt64())
}

func TestGenAIUsageCacheCreationInputTokens(t *testing.T) {
	kv := GenAIUsageCacheCreationInputTokens(32)
	assert.Equal(t, attribute.Key("gen_ai.usage.cache_creation.input_tokens"), kv.Key)
	assert.Equal(t, int64(32), kv.Value.AsInt64())
}

func TestGenAIProviderName(t *testing.T) {
	kv := GenAIProviderName("anthropic")
	assert.Equal(t, attribute.Key("gen_ai.provider.name"), kv.Key)
	assert.Equal(t, "anthropic", kv.Value.AsString())
}

func TestGenAIRequestMaxTokens(t *testing.T) {
	kv := GenAIRequestMaxTokens(1024)
	assert.Equal(t, attribute.Key("gen_ai.request.max_tokens"), kv.Key)
	assert.Equal(t, int64(1024), kv.Value.AsInt64())
}

func TestGenAIRequestTemperature(t *testing.T) {
	kv := GenAIRequestTemperature(0.7)
	assert.Equal(t, attribute.Key("gen_ai.request.temperature"), kv.Key)
	assert.InDelta(t, 0.7, kv.Value.AsFloat64(), 1e-9)
}

func TestGenAIRequestTopP(t *testing.T) {
	kv := GenAIRequestTopP(0.9)
	assert.Equal(t, attribute.Key("gen_ai.request.top_p"), kv.Key)
	assert.InDelta(t, 0.9, kv.Value.AsFloat64(), 1e-9)
}

func TestGenAIRequestTopK(t *testing.T) {
	kv := GenAIRequestTopK(40)
	assert.Equal(t, attribute.Key("gen_ai.request.top_k"), kv.Key)
	assert.Equal(t, int64(40), kv.Value.AsInt64())
}

func TestGenAIRequestIsStream(t *testing.T) {
	kv := GenAIRequestIsStream(true)
	assert.Equal(t, attribute.Key("gen_ai.request.is_stream"), kv.Key)
	assert.True(t, kv.Value.AsBool())
}

func TestGenAIResponseTimeToFirstToken(t *testing.T) {
	kv := GenAIResponseTimeToFirstToken(1500)
	assert.Equal(t, attribute.Key("gen_ai.response.time_to_first_token"), kv.Key)
	assert.Equal(t, int64(1500), kv.Value.AsInt64())
}
