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
	stream  = flag.Bool("stream", false, "Use OpenAI chat completion streaming API")
)

func main() {
	flag.Parse()

	client := openai.NewClient(
		option.WithBaseURL(*baseURL),
		option.WithAPIKey(*apiKey),
		option.WithMaxRetries(0),
	)

	ctx := context.Background()

	if *stream {
		stream := client.Chat.Completions.NewStreaming(ctx, openai.ChatCompletionNewParams{
			Model: "gpt-4",
			Messages: []openai.ChatCompletionMessageParamUnion{
				openai.UserMessage("Say hello"),
			},
			StreamOptions: openai.ChatCompletionStreamOptionsParam{
				IncludeUsage: openai.Bool(true),
			},
		})
		defer stream.Close()
		for stream.Next() {
			chunk := stream.Current()
			slog.Info("chat completion chunk", "id", chunk.ID, "model", chunk.Model)
		}
		if err := stream.Err(); err != nil {
			log.Printf("chat completion stream error (expected in test): %v", err)
		}
		return
	}

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
