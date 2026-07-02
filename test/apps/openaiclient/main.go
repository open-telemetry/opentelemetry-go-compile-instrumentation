// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a minimal OpenAI client for integration testing.
// This client is designed to be instrumented with the otelc compile-time tool.
package main

import (
	"context"
	"flag"
	"log"
	"log/slog"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

var (
	addr   = flag.String("addr", "http://localhost:8080/v1", "The OpenAI API base URL")
	apiKey = flag.String("api-key", "test-key", "The API key")
	model  = flag.String("model", "gpt-4", "The model to use")
)

func main() {
	flag.Parse()

	client := openai.NewClient(
		option.WithBaseURL(*addr),
		option.WithAPIKey(*apiKey),
	)

	completion, err := client.Chat.Completions.New(context.Background(), openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage("Say hello in one word"),
		},
		Model: openai.ChatModel(*model),
	})
	if err != nil {
		log.Fatalf("failed to create chat completion: %v", err)
	}

	slog.Info("response", "content", completion.Choices[0].Message.Content)
}
