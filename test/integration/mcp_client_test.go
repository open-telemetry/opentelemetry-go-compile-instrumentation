// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"go.opentelemetry.io/otelc/test/testutil"
)

// TestMCPClient exercises the instrumented MCP client binary against an
// in-memory MCP server. It covers the success path (with W3C traceparent
// propagation through the request _meta), a logical tool failure
// (CallToolResult.IsError), a JSON-RPC failure (an unknown tool) and the
// disabled-instrumentation path. The jsonrpc.request.id assertions also verify
// the AfterConnectionCall receiver/context parameter indexing and that the id
// is attached only to the CallTool span this instrumentation started.
func TestMCPClient(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "mcpclient", "go", "build", "-a")

	t.Run("Success", func(t *testing.T) {
		f := testutil.NewTestFixture(t)
		out := f.Run("mcpclient", "-mode=ok")
		require.Contains(t, out, "mcp tool call succeeded")

		span := testutil.RequireSpan(t, f.Traces(),
			testutil.HasName("tools/call echo"),
			testutil.IsClient,
		)
		testutil.RequireMCPClientSemconv(t, span, "echo")
		assert.Equal(t, ptrace.StatusCodeUnset, span.Status().Code())

		// BeforeCallTool injects the W3C traceparent into the request _meta;
		// the in-process server echoes it back, so it must carry the client
		// span's trace id.
		re := regexp.MustCompile(`traceparent=\d{2}-([0-9a-f]{32})-[0-9a-f]{16}-\d{2}`)
		match := re.FindStringSubmatch(out)
		require.NotNil(t, match, "output must echo the injected traceparent: %q", out)
		assert.Equal(t, span.TraceID().String(), match[1])
	})

	t.Run("ToolError", func(t *testing.T) {
		f := testutil.NewTestFixture(t)
		out := f.Run("mcpclient", "-mode=tool-error")
		require.Contains(t, out, "mcp tool result reported a tool error")

		span := testutil.RequireSpan(t, f.Traces(),
			testutil.HasName("tools/call fail"),
			testutil.IsClient,
		)
		testutil.RequireMCPClientSemconv(t, span, "fail")
		assert.Equal(t, ptrace.StatusCodeError, span.Status().Code())
		testutil.RequireAttribute(t, span, "error.type", "tool_error")
	})

	t.Run("JSONRPCError", func(t *testing.T) {
		f := testutil.NewTestFixture(t)
		out := f.Run("mcpclient", "-mode=rpc-error")
		require.Contains(t, out, "mcp tool call returned a JSON-RPC error")
		require.Contains(t, out, "unknown tool")

		span := testutil.RequireSpan(t, f.Traces(),
			testutil.HasName("tools/call missing"),
			testutil.IsClient,
		)
		testutil.RequireMCPClientSemconv(t, span, "missing")
		assert.Equal(t, ptrace.StatusCodeError, span.Status().Code())
		// The unknown-tool failure is a JSON-RPC error: error.type and
		// rpc.response.status_code both carry the same bare wire code.
		errType, ok := testutil.Attrs(span)["error.type"].(string)
		require.True(t, ok)
		testutil.RequireAttribute(t, span, "rpc.response.status_code", errType)
	})

	t.Run("Disabled", func(t *testing.T) {
		f := testutil.NewTestFixture(t)
		f.SetEnv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "mcp")
		out := f.Run("mcpclient", "-mode=ok")
		require.Contains(t, out, "mcp tool call succeeded")

		// With the instrumentation disabled no CallTool span is created and
		// the request _meta carries no traceparent.
		assert.Empty(t, testutil.AllSpans(f.Traces()))
		assert.NotContains(t, out, "traceparent=00-")
	})
}
