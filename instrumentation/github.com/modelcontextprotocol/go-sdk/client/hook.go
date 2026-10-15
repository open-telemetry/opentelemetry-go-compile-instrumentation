// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
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
	tracer     trace.Tracer
	propagator propagation.TextMapPropagator
	initOnce   sync.Once
)

func initInstrumentation() {
	initOnce.Do(func() {
		tracer = otel.GetTracerProvider().Tracer(
			instrumentationName,
			trace.WithInstrumentationVersion(runtime.ModuleVersion()),
		)
		propagator = otel.GetTextMapPropagator()
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
// Signature: func (cs *mcp.ClientSession) CallTool(ctx, *mcp.CallToolParams)
// (*mcp.CallToolResult, error), so the parameter list seen by the hook framework
// is: receiver cs = param 0, ctx = param 1, params = param 2. SetParam targets
// those framework-level indices, not the hook function's own argument list.
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
			ToolName:  params.Name,
			SessionID: sessionID(cs),
		})...),
	)
	// Make downstream calls (and the real CallTool body) use the span context.
	ictx.SetParam(1, spanCtx)

	// Propagate the trace context through the JSON-RPC _meta field so a
	// server-side handler can continue this trace. Copy params and the Meta
	// map before mutating them: the caller's objects must stay untouched.
	injected := *params
	meta := make(mcp.Meta, len(params.Meta)+2)
	for k, v := range params.Meta {
		meta[k] = v
	}
	propagator.Inject(spanCtx, metaCarrier(meta))
	injected.Meta = meta
	ictx.SetParam(2, &injected)

	ictx.SetData(span)
}

// sessionID returns the MCP session identifier for the client session, or ""
// when the connection has no session id yet (e.g. before the initialize
// handshake completes).
func sessionID(cs *mcp.ClientSession) string {
	if cs == nil {
		return ""
	}
	return cs.ID()
}

// metaCarrier adapts mcp.Meta (a map[string]any) to the TextMapCarrier
// interface so the W3C trace context can be injected into the request _meta.
type metaCarrier mcp.Meta

func (c metaCarrier) Get(key string) string {
	if v, ok := c[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func (c metaCarrier) Set(key, value string) {
	c[key] = value
}

func (c metaCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

// AfterCallTool finalizes the span started by BeforeCallTool.
//
// MCP distinguishes two failure shapes: a transport/JSON-RPC error (the err
// return) and a successful response that reports a logical tool failure via
// CallToolResult.IsError. Both mark the span as ERROR and both set error.type:
// the IsError case uses the well-known value "tool_error" mandated by the
// GenAI semantic conventions; a JSON-RPC error uses its wire code (e.g.
// "jsonrpc_-32601"), and any other (transport) error uses the stable
// low-cardinality value "transport_error".
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
		var rpcErr *jsonrpc.Error
		if errors.As(err, &rpcErr) {
			span.SetAttributes(attribute.String("error.type", fmt.Sprintf("jsonrpc_%d", rpcErr.Code)))
		} else {
			span.SetAttributes(attribute.String("error.type", "transport_error"))
		}
	case result != nil && result.IsError:
		span.SetAttributes(attribute.String("error.type", mcpsemconv.ToolError))
		span.SetStatus(codes.Error, mcpsemconv.ToolError)
	}
}
