// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a minimal MCP client for integration testing. It
// connects an in-memory MCP server and performs one tools/call, so the
// instrumented binary exercises the client-session hook path end to end.
package main

import (
	"context"
	"flag"
	"log"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var toolName = flag.String("tool", "get-weather", "tool name to call")

func main() {
	flag.Parse()

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "v0.0.1"}, nil)
	server := mcp.NewServer(&mcp.Implementation{Name: "server", Version: "v0.0.1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: *toolName},
		func(ctx context.Context, req *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "sunny"}},
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

	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: *toolName})
	if err != nil {
		log.Fatalf("call tool: %v", err)
	}
	slog.Info("tool result", "isError", result.IsError)
}
