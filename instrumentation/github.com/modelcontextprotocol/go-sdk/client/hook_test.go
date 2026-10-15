// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

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

	// The caller's original Meta map must be untouched (the injected copy is a
	// separate map passed back through SetParam).
	assert.NotContains(t, origMeta, "traceparent")
	assert.Equal(t, "value", origMeta["existing"])
	assert.Equal(t, &origMeta, &params.Meta, "params struct itself must not be mutated")
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
	assert.Equal(t, "jsonrpc_-32601", spanAttrs(spans[0])["error.type"])
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
