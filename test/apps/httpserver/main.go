// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a minimal HTTP server for integration testing.
// This server is designed to be instrumented with the otel compile-time tool.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
)

var port = flag.String("port", "8080", "The server port")

func greetHandler(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if err := json.NewEncoder(w).Encode("Hello " + name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func main() {
	flag.Parse()

	addr := fmt.Sprintf(":%s", *port)
	http.HandleFunc("/hello", greetHandler)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
