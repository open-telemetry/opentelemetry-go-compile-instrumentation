// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a minimal OpenAI client for integration testing.
// This client is designed to be instrumented with the otelc compile-time tool.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

var (
	addr   = flag.String("addr", "http://localhost:8080/v1", "The OpenAI API base URL")
	apiKey = flag.String("api-key", "test-key", "The API key")
	model  = flag.String("model", "gpt-4", "The model to use")
)

func run() error {
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
		return fmt.Errorf("failed to create chat completion: %w", err)
	}

	slog.Info("response", "content", completion.Choices[0].Message.Content)
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Printf("error: %v\n", err)
		os.Exit(1)
	}
}
