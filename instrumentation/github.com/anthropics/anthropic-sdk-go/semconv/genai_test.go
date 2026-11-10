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
