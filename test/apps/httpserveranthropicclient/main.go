// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main is a complex demo app combining HTTP server and Anthropic client for e2e testing.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

var (
	frontPort = flag.Int("front-port", 8080, "port for HTTP frontend")
	addr      = flag.String("addr", "http://localhost:8080", "The Anthropic API base URL")
	apiKey    = flag.String("api-key", "test-key", "The API key")
	model     = flag.String("model", "claude-sonnet-4-5", "The model to use")
)

func main() {
	flag.Parse()

	client := anthropic.NewClient(
		option.WithBaseURL(*addr),
		option.WithAPIKey(*apiKey),
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
		message, err := client.Messages.New(r.Context(), anthropic.MessageNewParams{
			Model:     anthropic.Model(*model),
			MaxTokens: 1024,
			Messages: []anthropic.MessageParam{
				anthropic.NewUserMessage(anthropic.NewTextBlock("Say hello in one word")),
			},
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		for _, block := range message.Content {
			if text, ok := block.AsAny().(anthropic.TextBlock); ok {
				_, _ = w.Write([]byte(text.Text))
			}
		}
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", *frontPort),
		Handler: mux,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("frontend server failed: %v", err)
		}
	}()

	// Wait for shutdown signal
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("server shutdown failed: %v", err)
	}
}
