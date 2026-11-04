// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
