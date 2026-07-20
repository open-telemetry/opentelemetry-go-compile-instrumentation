// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a demo Anthropic client showing compile-time
// instrumentation with OpenTelemetry. It connects to an Anthropic-compatible
// API and generates a message, producing GenAI semantic convention spans
// automatically.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

var (
	addr     = flag.String("addr", "", "The Anthropic API base URL (leave empty for default)")
	apiKey   = flag.String("api-key", "", "The API key (defaults to ANTHROPIC_API_KEY env)")
	model    = flag.String("model", "claude-sonnet-4-5", "The model to use")
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
		key = os.Getenv("ANTHROPIC_API_KEY")
	}
	if key != "" {
		opts = append(opts, option.WithAPIKey(key))
	}
	if *addr != "" {
		opts = append(opts, option.WithBaseURL(*addr))
	}

	client := anthropic.NewClient(opts...)

	logger.Info("sending message request",
		"model", *model,
		"prompt", *prompt)

	message, err := client.Messages.New(context.Background(), anthropic.MessageNewParams{
		Model:     anthropic.Model(*model),
		MaxTokens: 1024,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(*prompt)),
		},
	})
	if err != nil {
		logger.Error("message request failed", "error", err)
		os.Exit(1)
	}

	var content string
	for _, block := range message.Content {
		if text, ok := block.AsAny().(anthropic.TextBlock); ok {
			content = text.Text
			break
		}
	}

	if content != "" {
		logger.Info("message request succeeded",
			"model", message.Model,
			"content", content,
			"usage_input_tokens", message.Usage.InputTokens,
			"usage_output_tokens", message.Usage.OutputTokens)
		fmt.Println(content)
	} else {
		logger.Warn("no text content in response")
	}
}
