// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a demo OpenAI client showing compile-time instrumentation
// with OpenTelemetry. It connects to an OpenAI-compatible API and generates a chat
// completion, producing GenAI semantic convention spans automatically.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

var (
	addr     = flag.String("addr", "", "The OpenAI API base URL (leave empty for default)")
	apiKey   = flag.String("api-key", "", "The API key (defaults to OPENAI_API_KEY env)")
	model    = flag.String("model", "gpt-4", "The model to use")
	prompt   = flag.String("prompt", "Say hello in one word", "The prompt to send")
	logLevel = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
)

func main() {
	defer func() {
		// Wait for OpenTelemetry SDK to flush spans before exit
		time.Sleep(2 * time.Second)
	}()

	flag.Parse()

	// Initialize logger
	var level slog.Level
	switch *logLevel {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

	// Build client options
	var opts []option.RequestOption

	key := *apiKey
	if key == "" {
		key = os.Getenv("OPENAI_API_KEY")
	}
	if key != "" {
		opts = append(opts, option.WithAPIKey(key))
	}
	if *addr != "" {
		opts = append(opts, option.WithBaseURL(*addr))
	}

	client := openai.NewClient(opts...)

	logger.Info("sending chat completion request",
		"model", *model,
		"prompt", *prompt)

	completion, err := client.Chat.Completions.New(context.Background(), openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage(*prompt),
		},
		Model: openai.ChatModel(*model),
	})
	if err != nil {
		logger.Error("chat completion failed", "error", err)
		os.Exit(1)
	}

	if len(completion.Choices) > 0 {
		content := completion.Choices[0].Message.Content
		logger.Info("chat completion succeeded",
			"model", completion.Model,
			"content", content,
			"usage_prompt_tokens", completion.Usage.PromptTokens,
			"usage_completion_tokens", completion.Usage.CompletionTokens,
			"usage_total_tokens", completion.Usage.TotalTokens)
		fmt.Println(content)
	} else {
		logger.Warn("no choices in response")
	}
}
