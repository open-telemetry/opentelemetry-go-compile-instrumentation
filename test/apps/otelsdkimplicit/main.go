// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a test application that does not import the OTel SDK.
// It serves one traced HTTP handler. Inside that handler, it starts a span from
// context.Background(), the way a library that never receives the request
// context does (for example GORM v1). otelc links the SDK trace package
// through its own runtime, so that span must join the server span's trace.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

var port = flag.String("port", "8990", "The server port")

// bgSpanHandler runs while the net/http server span is active on the handler
// goroutine. It starts the child span from context.Background(). It prints
// whether a server span is in GLS and whether the child span has the same
// trace ID. Both must match, even though this application never imports the
// SDK.
func bgSpanHandler(w http.ResponseWriter, _ *http.Request) {
	parent := trace.SpanFromContext(context.Background())

	_, span := otel.Tracer("implicit").Start(context.Background(), "bg-child-span")
	sameTrace := span.SpanContext().TraceID() == parent.SpanContext().TraceID()
	fmt.Printf("OTEL_SDK_IMPLICIT: parent-valid=%t same-trace=%t\n",
		parent.SpanContext().IsValid(), sameTrace)
	span.End()

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func main() {
	flag.Parse()
	addr := fmt.Sprintf(":%s", *port)

	http.HandleFunc("/otel", bgSpanHandler)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	go func() {
		if err := http.Serve(ln, nil); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%s/otel", *port))
	if err != nil {
		log.Fatalf("request to /otel failed: %v", err)
	}
	resp.Body.Close()

	// Give time for span export.
	time.Sleep(1 * time.Second)
}
