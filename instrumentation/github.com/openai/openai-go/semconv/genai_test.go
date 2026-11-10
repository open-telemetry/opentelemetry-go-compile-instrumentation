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

func TestGenAIRequestIsStream(t *testing.T) {
	kv := GenAIRequestIsStream(true)
	assert.Equal(t, attribute.Key("gen_ai.request.is_stream"), kv.Key)
	assert.True(t, kv.Value.AsBool())
}

func TestGenAIUsageTotalTokens(t *testing.T) {
	kv := GenAIUsageTotalTokens(150)
	assert.Equal(t, attribute.Key("gen_ai.usage.total_tokens"), kv.Key)
	assert.Equal(t, int64(150), kv.Value.AsInt64())
}

func TestGenAIResponseTimeToFirstToken(t *testing.T) {
	kv := GenAIResponseTimeToFirstToken(12345)
	assert.Equal(t, attribute.Key("gen_ai.response.time_to_first_token"), kv.Key)
	assert.Equal(t, int64(12345), kv.Value.AsInt64())
}
