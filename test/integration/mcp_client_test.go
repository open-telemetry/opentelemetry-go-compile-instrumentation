// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"go.opentelemetry.io/otelc/test/testutil"
)

// TestMCPClient exercises the instrumented MCP client binary: it connects an
// in-memory MCP server, performs one tools/call, and asserts on the exported
// client span, including the jsonrpc.request.id captured by the
// AfterConnectionCall hook (the receiver/context parameter indexing of
// (*Connection).Call).
func TestMCPClient(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "mcpclient", "go", "build", "-a")

	t.Run("CallTool", func(t *testing.T) {
		f := testutil.NewTestFixture(t)

		out := f.Run("mcpclient")
		require.Contains(t, out, "tool result")

		span := f.RequireSingleSpan()
		require.Equal(t, "tools/call get-weather", span.Name())
		require.Equal(t, ptrace.SpanKindClient, span.Kind())

		attrs := testutil.Attrs(span)
		require.Equal(t, "execute_tool", attrs["gen_ai.operation.name"])
		require.Equal(t, "tools/call", attrs["mcp.method.name"])
		require.Equal(t, "get-weather", attrs["gen_ai.tool.name"])
		// The AfterConnectionCall hook reads the span context from parameter
		// index 1 (request context) of (*Connection).Call; a real instrumented
		// binary must therefore record the JSON-RPC request id.
		require.NotEmpty(t, attrs["jsonrpc.request.id"])
	})
}
