// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides an OpenAI client demo for demonstrating OpenTelemetry
// compile-time instrumentation with openai-go.
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

const (
	requestDelayDuration = 2 * time.Second
)

var (
	apiKey   = flag.String("api-key", "", "OpenAI API key (or set OPENAI_API_KEY env var)")
	baseURL  = flag.String("base-url", "", "OpenAI API base URL (optional)")
	model    = flag.String("model", "gpt-4", "Model to use for chat completions")
	count    = flag.Int("count", 1, "Number of iterations to run")
	logLevel = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	logger   *slog.Logger
)

func runChatCompletion(ctx context.Context, client *openai.Client, iteration int) error {
	prompt := fmt.Sprintf("Say hello in a creative way #%d", iteration)

	logger.Info("sending chat completion", "iteration", iteration, "model", *model)

	completion, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: openai.ChatModel(*model),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage(prompt),
		},
	})
	if err != nil {
		logger.Error("chat completion failed", "error", err)
		return err
	}

	if len(completion.Choices) > 0 {
		logger.Info("chat completion response",
			"id", completion.ID,
			"model", completion.Model,
			"content", completion.Choices[0].Message.Content,
			"finish_reason", completion.Choices[0].FinishReason,
			"input_tokens", completion.Usage.PromptTokens,
			"output_tokens", completion.Usage.CompletionTokens,
		)
	}

	return nil
}

func runEmbedding(ctx context.Context, client *openai.Client, iteration int) error {
	input := fmt.Sprintf("OpenTelemetry compile-time instrumentation demo #%d", iteration)

	logger.Info("sending embedding request", "iteration", iteration)

	resp, err := client.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Model: "text-embedding-3-small",
		Input: openai.EmbeddingNewParamsInputUnion{
			OfString: openai.String(input),
		},
	})
	if err != nil {
		logger.Error("embedding failed", "error", err)
		return err
	}

	logger.Info("embedding response",
		"model", resp.Model,
		"dimensions", len(resp.Data[0].Embedding),
		"input_tokens", resp.Usage.PromptTokens,
	)

	return nil
}

func main() {
	defer func() {
		// Wait for OpenTelemetry SDK to flush spans before exit
		time.Sleep(2 * time.Second)
	}()

	flag.Parse()

	// Initialize logger with appropriate level
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

	opts := &slog.HandlerOptions{
		Level: level,
	}
	logger = slog.New(slog.NewJSONHandler(os.Stdout, opts))

	// Resolve API key
	key := *apiKey
	if key == "" {
		key = os.Getenv("OPENAI_API_KEY")
	}
	if key == "" {
		logger.Error("API key required: use -api-key flag or OPENAI_API_KEY env var")
		os.Exit(1)
	}

	logger.Info("client starting",
		"model", *model,
		"request_count", *count,
		"log_level", *logLevel)

	// Create OpenAI client
	clientOpts := []option.RequestOption{
		option.WithAPIKey(key),
	}
	if *baseURL != "" {
		clientOpts = append(clientOpts, option.WithBaseURL(*baseURL))
	}
	client := openai.NewClient(clientOpts...)

	ctx := context.Background()

	successCount := 0
	failureCount := 0

	for i := 1; i <= *count; i++ {
		logger.Info("starting iteration",
			"iteration", i,
			"total", *count)

		// Run chat completion
		if err := runChatCompletion(ctx, &client, i); err != nil {
			failureCount++
			continue
		}

		// Run embedding
		if err := runEmbedding(ctx, &client, i); err != nil {
			failureCount++
			continue
		}

		successCount++

		// Add delay between iterations
		if i < *count {
			time.Sleep(requestDelayDuration)
		}
	}

	logger.Info("client finished",
		"total_iterations", *count,
		"successful", successCount,
		"failed", failureCount)
}
