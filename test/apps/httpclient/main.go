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
)

var (
	addr = flag.String("addr", "http://localhost:8080", "The server address")
	name = flag.String("name", "world", "The name to greet")
	path = flag.String("path", "", "The path to request (defaults to /hello?name={name})")
)

func main() {
	flag.Parse()

	var url string
	if *path != "" {
		url = fmt.Sprintf("%s%s", *addr, *path)
	} else {
		url = fmt.Sprintf("%s/hello?name=%s", *addr, *name)
	}
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
