// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a minimal Anthropic client for integration testing.
// This client is designed to be instrumented with the otelc compile-time tool.
package main

import (
	"context"
	"flag"
	"log"
	"log/slog"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

var (
	addr        = flag.String("addr", "http://localhost:8080", "The Anthropic API base URL")
	apiKey      = flag.String("api-key", "test-key", "The API key")
	model       = flag.String("model", "claude-sonnet-4-5", "The model to use")
	countTokens = flag.Bool("count-tokens", false, "Call Messages.CountTokens instead of Messages.New")
)

func main() {
	flag.Parse()

	client := anthropic.NewClient(
		option.WithBaseURL(*addr),
		option.WithAPIKey(*apiKey),
	)

	if *countTokens {
		count, err := client.Messages.CountTokens(context.Background(), anthropic.MessageCountTokensParams{
			Model: anthropic.Model(*model),
			Messages: []anthropic.MessageParam{
				anthropic.NewUserMessage(anthropic.NewTextBlock("Say hello in one word")),
			},
		})
		if err != nil {
			log.Fatalf("failed to count tokens: %v", err)
		}
		slog.Info("token count", "input_tokens", count.InputTokens)
		return
	}

	message, err := client.Messages.New(context.Background(), anthropic.MessageNewParams{
		Model:     anthropic.Model(*model),
		MaxTokens: 1024,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("Say hello in one word")),
		},
	})
	if err != nil {
		log.Fatalf("failed to create message: %v", err)
	}

	for _, block := range message.Content {
		if text, ok := block.AsAny().(anthropic.TextBlock); ok {
			slog.Info("response", "content", text.Text)
		}
	}
}
