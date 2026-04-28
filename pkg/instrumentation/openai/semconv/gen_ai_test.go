// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

func attrsToMap(attrs []attribute.KeyValue) map[string]interface{} {
	m := make(map[string]interface{})
	for _, attr := range attrs {
		m[string(attr.Key)] = attr.Value.AsInterface()
	}
	return m
}

func TestRequestTraceAttrs(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		model     string
		expected  map[string]interface{}
	}{
		{
			name:      "chat operation",
			operation: OperationChat,
			model:     "gpt-4",
			expected: map[string]interface{}{
				"gen_ai.system":         GenAISystemOpenAI,
				"gen_ai.operation.name": OperationChat,
				"gen_ai.request.model":  "gpt-4",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := RequestTraceAttrs(tt.operation, tt.model)
			m := attrsToMap(attrs)
			require.Len(t, m, len(tt.expected))
			for k, v := range tt.expected {
				assert.Equal(t, v, m[k], "attribute %s mismatch", k)
			}
		})
	}
}

func TestChatCompletionResponseTraceAttrs(t *testing.T) {
	attrs := ChatCompletionResponseTraceAttrs(
		"chatcmpl-abc123",
		"gpt-4-0613",
		[]string{"stop"},
		150,
		50,
	)
	m := attrsToMap(attrs)

	assert.Equal(t, "chatcmpl-abc123", m["gen_ai.response.id"])
	assert.Equal(t, "gpt-4-0613", m["gen_ai.response.model"])
	assert.Equal(t, int64(150), m["gen_ai.usage.input_tokens"])
	assert.Equal(t, int64(50), m["gen_ai.usage.output_tokens"])

	// finish_reasons is a string slice
	reasons, ok := m["gen_ai.response.finish_reasons"]
	require.True(t, ok, "expected finish_reasons attribute")
	assert.Equal(t, []string{"stop"}, reasons)
}

func TestChatCompletionResponseTraceAttrs_NoFinishReasons(t *testing.T) {
	attrs := ChatCompletionResponseTraceAttrs("id", "model", nil, 10, 20)
	m := attrsToMap(attrs)

	_, hasFinishReasons := m["gen_ai.response.finish_reasons"]
	assert.False(t, hasFinishReasons, "should not include finish_reasons when empty")
	assert.Len(t, m, 4) // id, model, input_tokens, output_tokens
}
