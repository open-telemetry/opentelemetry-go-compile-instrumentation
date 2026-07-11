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
	"net"
	"net/http"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	_ "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

var port = flag.String("port", "8989", "The server port")

var (
	workerTasks = make(chan func())
	startWorker sync.Once
	spanToEnd   = make(chan trace.Span)
	spanEnded   = make(chan struct{})
)

func submit(task func()) {
	startWorker.Do(func() {
		go func() {
			for task := range workerTasks {
				task()
			}
		}()
	})
	workerTasks <- task
}

func otelHandler(w http.ResponseWriter, r *http.Request) {
	_, span := otel.Tracer("handler").Start(context.Background(), "cross-goroutine-span")
	done := make(chan struct{})
	submit(func() {
		defer close(done)
		span := trace.SpanFromContext(context.Background())
		sc := span.SpanContext()

		if sc.IsValid() {
			fmt.Printf("OTEL_SDK_TEST: span valid, traceID=%s spanID=%s\n",
				sc.TraceID().String(), sc.SpanID().String())
		} else {
			fmt.Println("OTEL_SDK_TEST: span invalid")
		}
	})
	<-done
	spanToEnd <- span
	<-spanEnded

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func workerHandler(w http.ResponseWriter, r *http.Request) {
	done := make(chan struct{})
	submit(func() {
		defer close(done)
		stale := trace.SpanFromContext(context.Background()).SpanContext().IsValid()
		fmt.Printf("OTEL_SDK_WORKER: stale span=%t\n", stale)
		_, span := otel.Tracer("worker").Start(context.Background(), "worker-span")
		span.End()
	})
	<-done

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func main() {
	flag.Parse()
	addr := fmt.Sprintf(":%s", *port)

	http.HandleFunc("/otel", otelHandler)
	http.HandleFunc("/worker", workerHandler)
	go func() {
		span := <-spanToEnd
		span.End()
		close(spanEnded)
	}()

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	go func() {
		if err := http.Serve(ln, nil); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()

	for _, path := range []string{"otel", "worker"} {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%s/%s", *port, path))
		if err != nil {
			log.Fatalf("request to %s failed: %v", path, err)
		}
		resp.Body.Close()
	}

	// Give time for span export
	time.Sleep(1 * time.Second)
}
