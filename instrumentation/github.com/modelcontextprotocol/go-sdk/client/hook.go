// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"
	"sync"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	mcpsemconv "go.opentelemetry.io/otelc/instrumentation/github.com/modelcontextprotocol/go-sdk/semconv"
	"go.opentelemetry.io/otelc/pkg/hook"
	"go.opentelemetry.io/otelc/pkg/runtime"
)

const (
	instrumentationName = "go.opentelemetry.io/otelc/" +
		"instrumentation/github.com/modelcontextprotocol/go-sdk"
	instrumentationKey = "MCP"
)

// mcpEnablerImpl controls whether the MCP client instrumentation is enabled.
type mcpEnablerImpl struct{}

func (mcpEnablerImpl) Enable() bool {
	return runtime.Instrumented(instrumentationKey)
}

var mcpEnabler = mcpEnablerImpl{}

var (
	tracer   trace.Tracer
	initOnce sync.Once
)

func initInstrumentation() {
	initOnce.Do(func() {
		tracer = otel.GetTracerProvider().Tracer(
			instrumentationName,
			trace.WithInstrumentationVersion(runtime.ModuleVersion()),
		)
	})
}

// spanName renders the MCP client span name. The GenAI semantic conventions
// specify "{mcp.method.name} {tool}" for tool calls (e.g. "tools/call get-weather").
func spanName(toolName string) string {
	return mcpsemconv.MethodCallTool + " " + toolName
}

// BeforeCallTool starts a CLIENT span for an mcp.ClientSession.CallTool
// invocation. The span is named "tools/call <name>" and carries the GenAI/MCP
// attributes required by the semantic conventions.
//
// Signature: func (cs *mcp.ClientSession) CallTool(ctx, *mcp.CallToolParams) (*mcp.CallToolResult, error)
// so ctx is param 0 and params is param 1.
func BeforeCallTool(
	ictx hook.HookContext,
	cs *mcp.ClientSession,
	ctx context.Context,
	params *mcp.CallToolParams,
) {
	if !mcpEnabler.Enable() {
		return
	}
	if params == nil {
		return
	}
	initInstrumentation()

	spanCtx, span := tracer.Start(
		ctx,
		spanName(params.Name),
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(mcpsemconv.ToolCallTraceAttrs(mcpsemconv.ToolCallRequest{
			ToolName: params.Name,
		})...),
	)
	// Make downstream calls (and the real CallTool body) use the span context.
	ictx.SetParam(0, spanCtx)
	ictx.SetData(span)
}

// AfterCallTool finalizes the span started by BeforeCallTool.
//
// MCP distinguishes two failure shapes: a transport/JSON-RPC error (the err
// return) and a successful response that reports a logical tool failure via
// CallToolResult.IsError. Both mark the span as ERROR; the latter uses the
// well-known error.type "tool_error" mandated by the GenAI semantic
// conventions.
func AfterCallTool(
	ictx hook.HookContext,
	result *mcp.CallToolResult,
	err error,
) {
	span, ok := ictx.GetData().(trace.Span)
	if !ok || span == nil {
		return
	}
	defer span.End()

	switch {
	case err != nil:
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	case result != nil && result.IsError:
		span.SetAttributes(attribute.String("error.type", mcpsemconv.ToolError))
		span.SetStatus(codes.Error, mcpsemconv.ToolError)
	}
}
