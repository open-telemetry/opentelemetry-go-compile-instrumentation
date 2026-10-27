// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"
	"errors"
	"maps"
	"strconv"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
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

// sessionTransports records the detected network.transport value for each
// connected *mcp.ClientSession. It is populated by AfterConnect when the
// client establishes a session over a concrete Transport, and read by
// BeforeCallTool to annotate the tool-call span. Entries are removed by
// BeforeClose when the session is closed, so the map tracks only live sessions.
var sessionTransports sync.Map // *mcp.ClientSession -> string

// detectTransport maps a concrete mcp.Transport to the network.transport enum
// value from the OTel network semconv. It returns "" when the transport type
// cannot be determined (e.g. a generic IOTransport whose underlying stream is
// unknown), so the attribute is omitted rather than guessed.
func detectTransport(t mcp.Transport) string {
	switch tr := t.(type) {
	case *mcp.StdioTransport:
		return mcpsemconv.NetworkTransportPipe
	case *mcp.InMemoryTransport:
		return mcpsemconv.NetworkTransportInProc
	case *mcp.SSEClientTransport:
		return mcpsemconv.NetworkTransportTCP
	case *mcp.StreamableClientTransport:
		return mcpsemconv.NetworkTransportTCP
	case *mcp.LoggingTransport:
		return detectTransport(tr.Transport)
	default:
		return ""
	}
}

// BeforeConnect runs before (*Client).Connect(ctx, transport, opts). It stashes
// the detected transport type in the hook context so AfterConnect can associate
// it with the returned *ClientSession.
func BeforeConnect(ictx hook.HookContext, c *mcp.Client, ctx context.Context, t mcp.Transport, opts *mcp.ClientSessionOptions) {
	if !mcpEnabler.Enable() || t == nil {
		return
	}
	ictx.SetData(detectTransport(t))
}

// AfterConnect runs after (*Client).Connect returns. It associates the detected
// transport with the new session so BeforeCallTool can read it.
func AfterConnect(ictx hook.HookContext, cs *mcp.ClientSession, err error) {
	if err != nil || cs == nil {
		return
	}
	if tr, _ := ictx.GetData().(string); tr != "" {
		sessionTransports.Store(cs, tr)
	}
}

// BeforeClose runs before (*ClientSession).Close() is invoked. It removes the
// session's transport entry so the map does not accumulate dead sessions. An
// after hook cannot be used: Close() returns only error, so an after hook only
// receives (HookContext, error) and has no receiver, whereas a before hook
// receives HookContext plus the receiver.
func BeforeClose(ictx hook.HookContext, cs *mcp.ClientSession) {
	if cs != nil {
		sessionTransports.Delete(cs)
	}
}

// transportFor returns the recorded network.transport for a session, or ""
// when the session was not observed by AfterConnect (e.g. a zero-value
// ClientSession used in unit tests).
func transportFor(cs *mcp.ClientSession) string {
	if cs == nil {
		return ""
	}
	v, ok := sessionTransports.Load(cs)
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// callToolSpanKey marks the context that BeforeCallTool hands to the SDK for
// a tools/call request. The value is the client span the before hook started.
// Only AfterConnectionCall reads it, so the JSON-RPC request id is attached to
// the span this instrumentation started for the CallTool itself rather than
// whatever span happens to be current (e.g. the connect handshake or an
// application span surrounding the call).
type callToolSpanKey struct{}

// Positional parameters the CallTool hooks read and replace through the hook
// context for (cs *ClientSession) CallTool(ctx, params): receiver=0, ctx=1,
// params=2.
const (
	callToolParamContext = 1
	callToolParamParams  = 2

	// injectedMetaHeadroom reserves room in the copied _meta map for the
	// W3C trace-context entries the propagator injects (traceparent and,
	// when sampled, tracestate).
	injectedMetaHeadroom = 2
)

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
			Transport: transportFor(cs),
		})...),
	)
	// Make downstream calls (and the real CallTool body) use the span context,
	// and mark it so AfterConnectionCall can tell this span apart from the
	// caller's own ambient span.
	spanCtx = context.WithValue(spanCtx, callToolSpanKey{}, span)
	ictx.SetParam(callToolParamContext, spanCtx)

	// Propagate the trace context through the JSON-RPC _meta field so a
	// server-side handler can continue this trace. Copy params and the Meta
	// map before mutating them: the caller's objects must stay untouched.
	injected := *params
	meta := make(mcp.Meta, len(params.Meta)+injectedMetaHeadroom)
	maps.Copy(meta, params.Meta)
	propagator.Inject(spanCtx, metaCarrier(meta))
	injected.Meta = meta
	ictx.SetParam(callToolParamParams, &injected)

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
	v, found := c[key]
	if !found {
		return ""
	}
	s, isString := v.(string)
	if !isString {
		return ""
	}
	return s
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

// rpcCallID is the minimal surface the internal jsonrpc2.AsyncCall result of
// (*Connection).Call exposes for request-id capture. The result is an
// unexported concrete type, so the after hook asserts to this interface
// instead of importing the internal package.
type rpcCallID interface {
	ID() jsonrpc.ID
}

// requestIDString renders a JSON-RPC id the way it is sent on the wire:
// jsonrpc2 ids carry either an int64 or a string, and the attribute follows
// the jsonrpc convention's string representation.
func requestIDString(id jsonrpc.ID) string {
	switch v := id.Raw().(type) {
	case int64:
		return strconv.FormatInt(v, 10)
	case string:
		return v
	default:
		return ""
	}
}

// Positional parameters the (*jsonrpc2.Connection).Call hook sees: the
// receiver *Connection is parameter 0, ctx is 1 and method is 2.
const (
	connectionCallParamContext = 1
	connectionCallParamMethod  = 2
)

// AfterConnectionCall records jsonrpc.request.id on the client span that
// BeforeCallTool started for the enclosing ClientSession.CallTool.
//
// Every mcp request goes through internal/jsonrpc2.(*Connection).Call, but only
// the tools/call request must carry this attribute: initialize and the other
// JSON-RPC calls create no instrumentation span. The span is read through the
// private callToolSpanKey mark rather than trace.SpanFromContext, so an
// application span surrounding the call or another request reusing the span
// context is never annotated. For a method the receiver is parameter 0 and ctx
// is parameter 1, the same indices BeforeCallTool writes with. The return
// value is the unexported *jsonrpc2.AsyncCall, matched through the rpcCallID
// interface rather than importing the internal package.
func AfterConnectionCall(ictx hook.HookContext, result any) {
	call, ok := result.(rpcCallID)
	if !ok {
		return
	}
	if method, _ := ictx.GetParam(connectionCallParamMethod).(string); method != mcpsemconv.MethodCallTool {
		return
	}
	ctx, _ := ictx.GetParam(connectionCallParamContext).(context.Context)
	if ctx == nil {
		return
	}
	span, _ := ctx.Value(callToolSpanKey{}).(trace.Span)
	if span == nil || !span.SpanContext().IsValid() {
		return
	}
	if id := requestIDString(call.ID()); id != "" {
		span.SetAttributes(mcpsemconv.JSONRPCRequestID.String(id))
	}
}

// AfterCallTool finalizes the span started by BeforeCallTool.
//
// MCP distinguishes two failure shapes: a transport/JSON-RPC error (the err
// return) and a successful response that reports a logical tool failure via
// CallToolResult.IsError. Both mark the span as ERROR and both set error.type:
// the IsError case uses the well-known value "tool_error" mandated by the
// GenAI semantic conventions; a JSON-RPC error uses its bare wire code string
// (e.g. "-32601") for error.type and rpc.response.status_code, and any other
// (transport) error uses the stable low-cardinality value "transport_error".
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
		var rpcErr *jsonrpc.Error
		if errors.As(err, &rpcErr) {
			// JSON-RPC semconv: error.type carries the bare wire code string,
			// and rpc.response.status_code records the error.code as a string.
			code := strconv.FormatInt(rpcErr.Code, 10)
			span.SetAttributes(
				attribute.String("error.type", code),
				attribute.String("rpc.response.status_code", code),
			)
			span.SetStatus(codes.Error, rpcErr.Message)
		} else {
			span.SetAttributes(attribute.String("error.type", "transport_error"))
			span.SetStatus(codes.Error, err.Error())
		}
	case result != nil && result.IsError:
		span.SetAttributes(attribute.String("error.type", mcpsemconv.ToolError))
		span.SetStatus(codes.Error, mcpsemconv.ToolError)
	}
}
