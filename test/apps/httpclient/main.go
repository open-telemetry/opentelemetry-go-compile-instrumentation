// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a minimal HTTP client for integration testing.
// This client is designed to be instrumented with the otelc compile-time tool.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
)

var (
	addr = flag.String("addr", "http://localhost:8080", "The server address")
	name = flag.String("name", "world", "The name to greet")
)

func run() error {
	flag.Parse()

	url := fmt.Sprintf("%s/hello?name=%s", *addr, *name)
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	slog.Info("response", "body", string(body))
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Printf("error: %v\n", err)
		os.Exit(1)
	}
}
