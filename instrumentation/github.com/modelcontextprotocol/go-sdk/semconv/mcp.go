// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package semconv builds the OpenTelemetry attributes emitted by the
// compile-time instrumentation of the Model Context Protocol (MCP) Go SDK
// (github.com/modelcontextprotocol/go-sdk).
//
// The attributes follow the GenAI semantic conventions' MCP section
// (open-telemetry/semantic-conventions-genai, docs/gen-ai/mcp.md). That
// convention is still Development stability, so the attribute keys are declared
// here as constants rather than taken from a released semconv module.
package semconv

import (
	"go.opentelemetry.io/otel/attribute"
)

// MCP method names, per the MCP/JSON-RPC specification.
const (
	// MethodCallTool is the JSON-RPC method for invoking a tool.
	MethodCallTool = "tools/call"
)

// GenAI attribute keys from the Development GenAI semantic conventions.
const (
	// GenAIOperationName identifies the GenAI operation. Tool calls use
	// OperationExecuteTool.
	GenAIOperationName = attribute.Key("gen_ai.operation.name")
	// GenAIToolName is the name of the tool utilized by the agent.
	GenAIToolName = attribute.Key("gen_ai.tool.name")
)

// MCP attribute keys from the Development GenAI semantic conventions.
const (
	// MCPMethodName is the JSON-RPC request or notification method.
	MCPMethodName = attribute.Key("mcp.method.name")
)

// GenAI operation name values.
const (
	// OperationExecuteTool is the operation name for an MCP tool call.
	OperationExecuteTool = "execute_tool"

	// ToolError is the error.type value when a CallToolResult is returned
	// successfully over JSON-RPC but carries isError=true.
	ToolError = "tool_error"
)

// ToolCallRequest carries the information needed to build the attributes of a
// single MCP tools/call operation on the client side.
type ToolCallRequest struct {
	// ToolName is the name of the tool being invoked (CallToolParams.Name).
	ToolName string
}

// ToolCallTraceAttrs returns the trace attributes for a client-side tools/call
// span. The span identity is fixed by the convention: every MCP tool call
// carries the operation name, the JSON-RPC method, and the tool name.
//
// Call arguments and results are intentionally not emitted here: the GenAI
// convention marks gen_ai.tool.call.arguments / gen_ai.tool.call.result as
// Opt-In, because they may contain sensitive or large payloads. Recording them
// unconditionally on every call would leak user data by default.
func ToolCallTraceAttrs(req ToolCallRequest) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		GenAIOperationName.String(OperationExecuteTool),
		MCPMethodName.String(MethodCallTool),
	}
	if req.ToolName != "" {
		attrs = append(attrs, GenAIToolName.String(req.ToolName))
	}
	return attrs
}
