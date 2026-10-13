// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
)

func TestToolCallTraceAttrs(t *testing.T) {
	t.Run("all fields", func(t *testing.T) {
		attrs := ToolCallTraceAttrs(ToolCallRequest{ToolName: "get-weather"})
		m := asMap(attrs)
		assert.Equal(t, OperationExecuteTool, m["gen_ai.operation.name"])
		assert.Equal(t, MethodCallTool, m["mcp.method.name"])
		assert.Equal(t, "get-weather", m["gen_ai.tool.name"])
	})

	t.Run("empty tool name omits the attribute", func(t *testing.T) {
		attrs := ToolCallTraceAttrs(ToolCallRequest{})
		m := asMap(attrs)
		assert.Len(t, attrs, 2)
		assert.Equal(t, OperationExecuteTool, m["gen_ai.operation.name"])
		assert.Equal(t, MethodCallTool, m["mcp.method.name"])
		_, present := m["gen_ai.tool.name"]
		assert.False(t, present)
	})
}

func TestConstants(t *testing.T) {
	assert.Equal(t, "tools/call", MethodCallTool)
	assert.Equal(t, "execute_tool", OperationExecuteTool)
	assert.Equal(t, "tool_error", ToolError)
}

// asMap indexes attributes by key so a test can assert on individual values.
// It lives in this file rather than a separate helpers_test.go because every
// test file must pair 1:1 with a source file of the same name.
func asMap(attrs []attribute.KeyValue) map[string]any {
	m := make(map[string]any, len(attrs))
	for _, a := range attrs {
		m[string(a.Key)] = a.Value.AsInterface()
	}
	return m
}
