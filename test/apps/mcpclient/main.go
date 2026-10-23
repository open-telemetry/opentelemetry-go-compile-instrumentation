// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a minimal MCP client for integration testing. It
// connects an in-memory MCP server and issues a tools/call in one of three
// modes (ok, tool-error, rpc-error), so the instrumented binary exercises the
// client-session hooks across the success, logical tool failure and JSON-RPC
// failure paths end to end.
package main

import (
	"context"
	"flag"
	"log"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	mode := flag.String("mode", "ok", "call mode: ok|tool-error|rpc-error")
	flag.Parse()

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "v0.0.1"}, nil)
	server := mcp.NewServer(&mcp.Implementation{Name: "server", Version: "v0.0.1"}, nil)

	// Echo tool: returns the W3C traceparent the client injected into the
	// request _meta, so the integration test can prove context propagation.
	mcp.AddTool(server, &mcp.Tool{Name: "echo"},
		func(ctx context.Context, req *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
			if tp, ok := req.Params.Meta["traceparent"].(string); ok {
				return &mcp.CallToolResult{
					Content: []mcp.Content{&mcp.TextContent{Text: "traceparent=" + tp}},
				}, nil, nil
			}
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "no traceparent"}},
			}, nil, nil
		})

	// Fail tool: a successful JSON-RPC response whose result reports a logical
	// tool failure (IsError=true).
	mcp.AddTool(server, &mcp.Tool{Name: "fail"},
		func(ctx context.Context, req *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: "the tool failed"}},
			}, nil, nil
		})

	ct, st := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, st, nil)
	if err != nil {
		log.Fatalf("server connect: %v", err)
	}
	defer func() { _ = serverSession.Close() }()

	clientSession, err := client.Connect(ctx, ct, nil)
	if err != nil {
		log.Fatalf("client connect: %v", err)
	}
	defer func() { _ = clientSession.Close() }()

	tool := "echo"
	switch *mode {
	case "tool-error":
		tool = "fail"
	case "rpc-error":
		// No such tool is registered, so the server answers with a JSON-RPC
		// error.
		tool = "missing"
	}

	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: tool})
	switch {
	case err != nil:
		slog.Info("mcp tool call returned a JSON-RPC error", "tool", tool, "error", err)
		return
	case result != nil && result.IsError:
		slog.Info("mcp tool result reported a tool error", "tool", tool)
		return
	}

	if len(result.Content) > 0 {
		if tc, ok := result.Content[0].(*mcp.TextContent); ok {
			slog.Info("mcp tool call succeeded", "tool", tool, "content", tc.Text)
			return
		}
	}
	slog.Info("mcp tool call succeeded", "tool", tool)
}
