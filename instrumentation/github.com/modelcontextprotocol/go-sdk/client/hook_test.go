// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	mcpsemconv "go.opentelemetry.io/otelc/instrumentation/github.com/modelcontextprotocol/go-sdk/semconv"
	"go.opentelemetry.io/otelc/pkg/hook/hooktest"
)

func setupTest(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "mcp")

	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))

	initOnce.Do(func() {})
	tracer = tp.Tracer("test")
	propagator = propagation.TraceContext{}

	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		initOnce = sync.Once{}
		tracer = nil
		propagator = nil
	})
	return sr
}

func spanAttrs(span sdktrace.ReadOnlySpan) map[string]any {
	m := make(map[string]any)
	for _, a := range span.Attributes() {
		m[string(a.Key)] = a.Value.AsInterface()
	}
	return m
}

func TestCallTool_StartsClientSpan(t *testing.T) {
	sr := setupTest(t)

	params := &mcp.CallToolParams{Name: "get-weather"}
	ctx := context.Background()
	ictx := hooktest.NewMockHookContext(&mcp.ClientSession{}, ctx, params)

	BeforeCallTool(ictx, &mcp.ClientSession{}, ctx, params)

	// The hook framework parameter list is receiver=0, ctx=1, params=2: the
	// context handed to the real call must carry the new span, and the params
	// slot must hold the meta-injected copy.
	newCtx, ok := ictx.GetParam(1).(context.Context)
	require.True(t, ok)
	gotSpan := trace.SpanFromContext(newCtx)
	require.True(t, gotSpan.SpanContext().IsValid())

	gotParams, ok := ictx.GetParam(2).(*mcp.CallToolParams)
	require.True(t, ok)

	AfterCallTool(ictx, &mcp.CallToolResult{}, nil)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, "tools/call get-weather", spans[0].Name())
	assert.Equal(t, trace.SpanKindClient, spans[0].SpanKind())

	m := spanAttrs(spans[0])
	assert.Equal(t, "execute_tool", m["gen_ai.operation.name"])
	assert.Equal(t, "tools/call", m["mcp.method.name"])
	assert.Equal(t, "get-weather", m["gen_ai.tool.name"])
	assert.Equal(t, codes.Unset, spans[0].Status().Code)

	// The trace context must be injected into _meta so a server-side handler
	// can continue the trace.
	tp := gotParams.Meta["traceparent"]
	require.NotNil(t, tp)
	tpStr, ok := tp.(string)
	require.True(t, ok)
	assert.Contains(t, tpStr, gotSpan.SpanContext().TraceID().String())
}

func TestCallTool_MetaInjectionDoesNotMutateCaller(t *testing.T) {
	setupTest(t)

	params := &mcp.CallToolParams{
		Name: "x",
		Meta: mcp.Meta{"existing": "value"},
	}
	origMeta := params.Meta

	BeforeCallTool(hooktest.NewMockHookContext(&mcp.ClientSession{}, context.Background(), params),
		&mcp.ClientSession{}, context.Background(), params)

	// The caller's original Meta map must be untouched: the injected copy is a
	// separate map, and the params value passed back through SetParam must be
	// a different struct, never the caller's. Comparing values is not enough
	// (it passes even when the caller's map itself was mutated).
	assert.NotContains(t, origMeta, "traceparent")
	assert.Equal(t, "value", origMeta["existing"])
	injected, ok := params.Meta["traceparent"]
	_ = injected
	require.False(t, ok, "the caller's own Meta map must not receive the traceparent")
}

func mustID(t *testing.T, v any) jsonrpc.ID {
	t.Helper()
	id, err := jsonrpc.MakeID(v)
	require.NoError(t, err)
	return id
}

// fakeAsyncCall implements the minimal surface of internal
// jsonrpc2.AsyncCall: AfterConnectionCall only needs ID(). It stands in for
// the unexported result type the real hook receives.
type fakeAsyncCall struct {
	id jsonrpc.ID
}

func (c *fakeAsyncCall) ID() jsonrpc.ID { return c.id }

// fakeConnection stands in for the unexported *jsonrpc2.Connection receiver.
type fakeConnection struct{}

// connectionCall mirrors the hook context the trampoline builds for
// (*jsonrpc2.Connection).Call(ctx, method, params): the receiver is parameter
// 0, followed by ctx, method and params.
func connectionCall(ctx context.Context, method string) *hooktest.MockHookContext {
	return hooktest.NewMockHookContext(&fakeConnection{}, ctx, method, nil)
}

// startCallTool runs the CallTool before hook and returns its hook context
// together with the context it hands to the SDK, which the SDK passes on to
// (*jsonrpc2.Connection).Call for the tools/call request.
func startCallTool(t *testing.T, tool string) (*hooktest.MockHookContext, context.Context) {
	t.Helper()
	params := &mcp.CallToolParams{Name: tool}
	ictx := hooktest.NewMockHookContext(&mcp.ClientSession{}, context.Background(), params)
	BeforeCallTool(ictx, &mcp.ClientSession{}, context.Background(), params)
	ctx, ok := ictx.GetParam(1).(context.Context)
	require.True(t, ok)
	return ictx, ctx
}

func TestAfterConnectionCall_RecordsRequestID(t *testing.T) {
	for _, tc := range []struct {
		name string
		id   any
		want string
	}{
		{name: "integer id is rendered as its wire string", id: float64(42), want: "42"},
		{name: "string id is passed through", id: "req-7", want: "req-7"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sr := setupTest(t)
			before, callCtx := startCallTool(t, "x")

			AfterConnectionCall(connectionCall(callCtx, "tools/call"), &fakeAsyncCall{id: mustID(t, tc.id)})
			AfterCallTool(before, &mcp.CallToolResult{}, nil)

			spans := sr.Ended()
			require.Len(t, spans, 1)
			assert.Equal(t, tc.want, spanAttrs(spans[0])["jsonrpc.request.id"])
		})
	}
}

func TestAfterConnectionCall_AnnotatesOnlyItsOwnToolCall(t *testing.T) {
	sr := setupTest(t)
	appCtx, appSpan := tracer.Start(context.Background(), "agent-run")
	before, callCtx := startCallTool(t, "x")

	// A request sent under a span the instrumentation did not start: the
	// connect handshake, or a tools/call the before hook skipped.
	AfterConnectionCall(connectionCall(appCtx, "tools/call"), &fakeAsyncCall{id: mustID(t, float64(1))})
	// Another request sent with the tools/call context, e.g. by a sending
	// middleware.
	AfterConnectionCall(connectionCall(callCtx, "tools/list"), &fakeAsyncCall{id: mustID(t, float64(2))})

	AfterCallTool(before, &mcp.CallToolResult{}, nil)
	appSpan.End()

	spans := sr.Ended()
	require.Len(t, spans, 2)
	for _, span := range spans {
		assert.NotContains(t, spanAttrs(span), "jsonrpc.request.id", "span %q", span.Name())
	}
}

func TestAfterConnectionCall_IgnoresUnusableInputs(t *testing.T) {
	for _, tc := range []struct {
		name   string
		ctx    func(callCtx context.Context) context.Context
		result any
	}{
		{
			name:   "result is not an AsyncCall",
			ctx:    func(callCtx context.Context) context.Context { return callCtx },
			result: "not an async call",
		},
		{
			name:   "nil context",
			ctx:    func(context.Context) context.Context { return nil },
			result: &fakeAsyncCall{id: mustID(t, float64(3))},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sr := setupTest(t)
			before, callCtx := startCallTool(t, "x")

			AfterConnectionCall(connectionCall(tc.ctx(callCtx), "tools/call"), tc.result)
			AfterCallTool(before, &mcp.CallToolResult{}, nil)

			spans := sr.Ended()
			require.Len(t, spans, 1)
			assert.NotContains(t, spanAttrs(spans[0]), "jsonrpc.request.id")
		})
	}
}

func TestCallTool_TransportError(t *testing.T) {
	sr := setupTest(t)

	params := &mcp.CallToolParams{Name: "x"}
	ictx := hooktest.NewMockHookContext(&mcp.ClientSession{}, context.Background(), params)
	BeforeCallTool(ictx, &mcp.ClientSession{}, context.Background(), params)

	AfterCallTool(ictx, nil, assertError("boom"))

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status().Code)
	assert.Contains(t, spans[0].Status().Description, "boom")
	assert.Equal(t, "transport_error", spanAttrs(spans[0])["error.type"])
}

func TestCallTool_JSONRPCErrorCarriesCode(t *testing.T) {
	sr := setupTest(t)

	params := &mcp.CallToolParams{Name: "x"}
	ictx := hooktest.NewMockHookContext(&mcp.ClientSession{}, context.Background(), params)
	BeforeCallTool(ictx, &mcp.ClientSession{}, context.Background(), params)

	AfterCallTool(ictx, nil, &jsonrpc.Error{Code: jsonrpc.CodeMethodNotFound, Message: "no such tool"})

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status().Code)
	attrs := spanAttrs(spans[0])
	// JSON-RPC semconv: error.type carries the bare wire code string, and
	// rpc.response.status_code records the error.code as a string.
	assert.Equal(t, "-32601", attrs["error.type"])
	assert.Equal(t, "-32601", attrs["rpc.response.status_code"])
	// The span status description uses the JSON-RPC message, not the wrapped error.
	assert.Equal(t, "no such tool", spans[0].Status().Description)
}

func TestCallTool_IsErrorResult(t *testing.T) {
	sr := setupTest(t)

	params := &mcp.CallToolParams{Name: "x"}
	ictx := hooktest.NewMockHookContext(&mcp.ClientSession{}, context.Background(), params)
	BeforeCallTool(ictx, &mcp.ClientSession{}, context.Background(), params)

	AfterCallTool(ictx, &mcp.CallToolResult{IsError: true}, nil)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status().Code)
	assert.Equal(t, "tool_error", spanAttrs(spans[0])["error.type"])
}

func TestCallTool_DisabledOrNilParams(t *testing.T) {
	sr := setupTest(t)
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "not-mcp")

	ictx := hooktest.NewMockHookContext(&mcp.ClientSession{}, context.Background(), nil)
	BeforeCallTool(ictx, &mcp.ClientSession{}, context.Background(), nil)
	// Nothing was stashed, so the after hook is a no-op too.
	AfterCallTool(ictx, &mcp.CallToolResult{}, nil)

	assert.Empty(t, sr.Ended())
}

type staticError string

func (e staticError) Error() string { return string(e) }

func assertError(msg string) error { return staticError(msg) }

func TestDetectTransport(t *testing.T) {
	for _, tc := range []struct {
		name string
		t    mcp.Transport
		want string
	}{
		{"stdio -> pipe", &mcp.StdioTransport{}, mcpsemconv.NetworkTransportPipe},
		{"IOTransport -> omitted", &mcp.IOTransport{}, ""},
		{"in-memory -> inproc", &mcp.InMemoryTransport{}, mcpsemconv.NetworkTransportInProc},
		{"SSE -> tcp", &mcp.SSEClientTransport{}, mcpsemconv.NetworkTransportTCP},
		{"streamable -> tcp", &mcp.StreamableClientTransport{}, mcpsemconv.NetworkTransportTCP},
		{"logging wraps stdio -> pipe", &mcp.LoggingTransport{Transport: &mcp.StdioTransport{}}, mcpsemconv.NetworkTransportPipe},
		{"logging wraps IOTransport -> omitted", &mcp.LoggingTransport{Transport: &mcp.IOTransport{}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, detectTransport(tc.t))
		})
	}
}

func TestNetworkTransport_EmittedAfterConnect(t *testing.T) {
	sr := setupTest(t)

	cs := &mcp.ClientSession{}
	connectCtx := hooktest.NewMockHookContext(&mcp.Client{}, context.Background(), &mcp.StdioTransport{}, (*mcp.ClientSessionOptions)(nil))
	BeforeConnect(connectCtx, &mcp.Client{}, context.Background(), &mcp.StdioTransport{}, nil)
	AfterConnect(connectCtx, cs, nil)

	params := &mcp.CallToolParams{Name: "get-weather"}
	ictx := hooktest.NewMockHookContext(cs, context.Background(), params)
	BeforeCallTool(ictx, cs, context.Background(), params)
	AfterCallTool(ictx, &mcp.CallToolResult{}, nil)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, mcpsemconv.NetworkTransportPipe, spanAttrs(spans[0])["network.transport"])

	// BeforeClose must remove the entry so the map does not leak.
	BeforeClose(ictx, cs)
	_, ok := sessionTransports.Load(cs)
	assert.False(t, ok, "session entry must be removed after Close")
}

func TestNetworkTransport_OmittedWhenNoConnectObserved(t *testing.T) {
	sr := setupTest(t)

	ictx := hooktest.NewMockHookContext(&mcp.ClientSession{}, context.Background(), &mcp.CallToolParams{Name: "x"})
	BeforeCallTool(ictx, &mcp.ClientSession{}, context.Background(), &mcp.CallToolParams{Name: "x"})
	AfterCallTool(ictx, &mcp.CallToolResult{}, nil)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.NotContains(t, spanAttrs(spans[0]), "network.transport")
}

// TestNetworkTransport_InMemoryEndToEnd runs the hook functions against a real
// in-memory MCP transport: Connect -> CallTool -> Close, verifying the span
// carries network.transport=inproc and the map entry is cleaned up.
func TestNetworkTransport_InMemoryEndToEnd(t *testing.T) {
	sr := setupTest(t)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test-server", Version: "1.0.0"}, nil)
	mcp.AddTool(srv, &mcp.Tool{Name: "ping"}, func(_ context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "pong"}}}, nil, nil
	})

	ctx := context.Background()
	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	defer serverSession.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1.0.0"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	require.NotNil(t, cs)

	// Run the connect hooks as the generated trampoline would.
	connectCtx := hooktest.NewMockHookContext(client, ctx, clientTransport, (*mcp.ClientSessionOptions)(nil))
	BeforeConnect(connectCtx, client, ctx, clientTransport, nil)
	AfterConnect(connectCtx, cs, nil)

	callParams := &mcp.CallToolParams{Name: "ping"}
	ictx := hooktest.NewMockHookContext(cs, ctx, callParams)
	BeforeCallTool(ictx, cs, ctx, callParams)
	result, callErr := cs.CallTool(ctx, callParams)
	AfterCallTool(ictx, result, callErr)
	require.NoError(t, callErr)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	m := spanAttrs(spans[0])
	assert.Equal(t, mcpsemconv.NetworkTransportInProc, m["network.transport"])

	require.NoError(t, cs.Close())
	BeforeClose(ictx, cs)
	_, ok := sessionTransports.Load(cs)
	assert.False(t, ok, "map entry must be removed after Close")
}
