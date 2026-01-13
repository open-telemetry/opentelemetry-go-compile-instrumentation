// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a minimal HTTP client for integration testing.
// This client is designed to be instrumented with the otel compile-time tool.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"time"
)

var (
	addr = flag.String("addr", "http://localhost:8080", "The server address")
	name = flag.String("name", "world", "The name to greet")
)

func main() {
	defer func() {
		// Wait for OpenTelemetry SDK to flush spans before exit
		time.Sleep(2 * time.Second)
	}()

	flag.Parse()

	url := fmt.Sprintf("%s/hello?name=%s", *addr, *name)
	resp, err := http.Get(url)
	if err != nil {
		log.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("failed to read response: %v", err)
	}

	slog.Info("response", "body", string(body))
}
