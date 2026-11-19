// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a test application that imports no OpenTelemetry
// package. The front handler calls a second local handler with plain http.Get,
// which uses context.Background(). otelc links the OTel API, the trace API,
// and the SDK trace package through its own runtime. The client span must
// therefore join the server span's trace.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
)

var port = flag.Int("port", 8080, "The server port")

// apiHandler is the second local handler. It answers the request that the
// front handler sends.
func apiHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// frontHandler runs inside the net/http server span. It calls the second
// handler with plain http.Get. http.Get uses context.Background(), the way a
// library that never receives the request context does. The client span must
// still join the server span's trace.
func frontHandler(w http.ResponseWriter, _ *http.Request) {
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/api", *port))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func main() {
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("/front", frontHandler)
	mux.HandleFunc("/api", apiHandler)

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	if err := http.Serve(ln, mux); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
