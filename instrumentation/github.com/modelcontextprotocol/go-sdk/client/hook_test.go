// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"
	"sync"
	"testing"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
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

	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		initOnce = sync.Once{}
		tracer = nil
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
	ictx := hooktest.NewMockHookContext((*mcp.ClientSession)(nil), ctx, params)

	BeforeCallTool(ictx, (*mcp.ClientSession)(nil), ctx, params)

	// The context handed to the real call must carry the new span, and params
	// is passed through unchanged.
	newCtx, ok := ictx.GetParam(0).(context.Context)
	require.True(t, ok)
	gotSpan := trace.SpanFromContext(newCtx)
	require.True(t, gotSpan.SpanContext().IsValid())

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
}

func TestCallTool_TransportError(t *testing.T) {
	sr := setupTest(t)

	params := &mcp.CallToolParams{Name: "x"}
	ictx := hooktest.NewMockHookContext((*mcp.ClientSession)(nil), context.Background(), params)
	BeforeCallTool(ictx, (*mcp.ClientSession)(nil), context.Background(), params)

	AfterCallTool(ictx, nil, assertError("boom"))

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status().Code)
	assert.Contains(t, spans[0].Status().Description, "boom")
}

func TestCallTool_IsErrorResult(t *testing.T) {
	sr := setupTest(t)

	params := &mcp.CallToolParams{Name: "x"}
	ictx := hooktest.NewMockHookContext((*mcp.ClientSession)(nil), context.Background(), params)
	BeforeCallTool(ictx, (*mcp.ClientSession)(nil), context.Background(), params)

	AfterCallTool(ictx, &mcp.CallToolResult{IsError: true}, nil)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status().Code)
	assert.Equal(t, "tool_error", spanAttrs(spans[0])["error.type"])
}

func TestCallTool_DisabledOrNilParams(t *testing.T) {
	sr := setupTest(t)
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "not-mcp")

	ictx := hooktest.NewMockHookContext((*mcp.ClientSession)(nil), context.Background(), nil)
	BeforeCallTool(ictx, (*mcp.ClientSession)(nil), context.Background(), nil)
	// Nothing was stashed, so the after hook is a no-op too.
	AfterCallTool(ictx, &mcp.CallToolResult{}, nil)

	assert.Empty(t, sr.Ended())
}

type staticError string

func (e staticError) Error() string { return string(e) }

func assertError(msg string) error { return staticError(msg) }
