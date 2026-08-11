// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a minimal OpenAI v2 SDK client for integration
// testing. Exercises the openai-go/v2 instrumentation package end-to-end,
// including its dependency on the shared internal/streaming module (a
// sibling of v2/v3 nested under v1's own directory), which openaiclient
// (v1) never reaches since it doesn't require any versioned SDK.
package main

import (
	"context"
	"flag"
	"log"
	"log/slog"

	openai "github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/option"
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
		Model: *model,
	})
	if err != nil {
		log.Fatalf("failed to create chat completion: %v", err)
	}

	slog.Info("response", "content", completion.Choices[0].Message.Content)
}
