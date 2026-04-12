// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a minimal OpenAI client for integration testing.
// This client is designed to be instrumented with the otelc compile-time tool.
// It makes a chat completion request against a configurable base URL.
package main

import (
	"context"
	"flag"
	"log"
	"log/slog"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

var (
	baseURL = flag.String("base-url", "http://localhost:8090/v1", "OpenAI API base URL")
	apiKey  = flag.String("api-key", "test-key", "OpenAI API key")
)

func main() {
	flag.Parse()

	client := openai.NewClient(
		option.WithBaseURL(*baseURL),
		option.WithAPIKey(*apiKey),
	)

	ctx := context.Background()

	// Chat completion request
	completion, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: "gpt-4",
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage("Say hello"),
		},
	})
	if err != nil {
		log.Printf("chat completion error (expected in test): %v", err)
	} else {
		slog.Info("chat completion", "id", completion.ID, "model", completion.Model)
	}
}
