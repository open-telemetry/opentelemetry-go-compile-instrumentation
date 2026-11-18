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
	"strings"

	"google.golang.org/genai"
)

var (
	addr   = flag.String("addr", "http://localhost:8080", "The Gemini API base URL")
	apiKey = flag.String("api-key", "test-key", "The API key")
	model  = flag.String("model", "gemini-2.5-flash", "The model to use")
	stream = flag.Bool("stream", false, "Use GenerateContentStream instead of GenerateContent")
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

	contents := genai.Text("Say hello in one word")
	config := &genai.GenerateContentConfig{MaxOutputTokens: 64}

	if *stream {
		var text strings.Builder
		for chunk, err := range client.Models.GenerateContentStream(ctx, *model, contents, config) {
			if err != nil {
				log.Fatalf("failed to stream content: %v", err)
			}
			text.WriteString(chunk.Text())
		}
		slog.Info("response", "content", text.String())
		return
	}

	resp, err := client.Models.GenerateContent(ctx, *model, contents, config)
	if err != nil {
		log.Fatalf("failed to generate content: %v", err)
	}

	slog.Info("response", "content", resp.Text())
}
