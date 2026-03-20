// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a test application that verifies OTel SDK instrumentation
// works correctly with GLS-based span propagation. It starts an HTTP server,
// sends a request to itself, and inside the handler verifies that
// trace.SpanFromContext(context.Background()) returns a valid span from GLS.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	_ "go.opentelemetry.io/otel"
	_ "go.opentelemetry.io/otel/baggage"
	_ "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

var port = flag.String("port", "8989", "The server port")

func otelHandler(w http.ResponseWriter, r *http.Request) {
	// Call SpanFromContext with a bare context.Background().
	// Without GLS instrumentation this returns a no-op span.
	// With the OTel SDK + net/http instrumentation, the GLS hook
	// should return the active span created by the server handler.
	span := trace.SpanFromContext(context.Background())
	sc := span.SpanContext()

	if sc.IsValid() {
		fmt.Printf("OTEL_SDK_TEST: span valid, traceID=%s spanID=%s\n",
			sc.TraceID().String(), sc.SpanID().String())
	} else {
		fmt.Println("OTEL_SDK_TEST: span invalid")
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func main() {
	flag.Parse()
	addr := fmt.Sprintf(":%s", *port)

	http.HandleFunc("/otel", otelHandler)

	go func() {
		if err := http.ListenAndServe(addr, nil); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()

	// Wait for the server to start
	time.Sleep(2 * time.Second)

	// Send a request to self
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%s/otel", *port))
	if err != nil {
		log.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()

	// Give time for span export
	time.Sleep(1 * time.Second)

}
