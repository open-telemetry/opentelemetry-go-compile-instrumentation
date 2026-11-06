// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a minimal Gemini client for integration testing.
// This client is designed to be instrumented with the otelc compile-time tool.
package main

import (
	"context"
	"flag"
	"log"
	"log/slog"

	"google.golang.org/genai"
)

var (
	addr   = flag.String("addr", "http://localhost:8080", "The Gemini API base URL")
	apiKey = flag.String("api-key", "test-key", "The API key")
	model  = flag.String("model", "gemini-2.5-flash", "The model to use")
)

func main() {
	flag.Parse()

	ctx := context.Background()

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:      *apiKey,
		Backend:     genai.BackendGeminiAPI,
		HTTPOptions: genai.HTTPOptions{BaseURL: *addr},
	})
	if err != nil {
		log.Fatalf("failed to create client: %v", err)
	}

	resp, err := client.Models.GenerateContent(ctx, *model,
		genai.Text("Say hello in one word"),
		&genai.GenerateContentConfig{MaxOutputTokens: 64},
	)
	if err != nil {
		log.Fatalf("failed to generate content: %v", err)
	}

	slog.Info("response", "content", resp.Text())
}
