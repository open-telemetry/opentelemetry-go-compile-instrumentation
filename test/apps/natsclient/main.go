// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a minimal nats.go client for integration testing.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"

	// The publish path has no context parameter to carry the caller's span, so
	// the app relies on the SDK trace instrumentation to maintain the active
	// span in goroutine-local storage; the nats.go instrumentation reads it
	// when starting the send span.
	_ "go.opentelemetry.io/otel/sdk/trace"
)

const subject = "orders.created"

var (
	natsURL   = flag.String("nats-url", "nats://127.0.0.1:4222", "NATS server URL")
	frontPort = flag.Int("front-port", 8080, "port for HTTP frontend")
)

func main() {
	flag.Parse()

	nc, err := nats.Connect(*natsURL)
	if err != nil {
		log.Fatalf("failed to connect: %v", err)
	}
	defer func() {
		_ = nc.Drain()
	}()

	// The async subscription is made before the frontend starts, so the
	// callback wrapper installed by the instrumentation is in place before any
	// message arrives. The callback itself only hands the message on; the
	// process span is created around it by the wrapper.
	received := make(chan *nats.Msg, 64)
	if _, err := nc.Subscribe(subject, func(m *nats.Msg) {
		received <- m
	}); err != nil {
		log.Fatalf("failed to subscribe: %v", err)
	}
	if err := nc.Flush(); err != nil {
		log.Fatalf("failed to flush: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/publish", func(w http.ResponseWriter, r *http.Request) {
		msg := nats.NewMsg(subject)
		msg.Data = []byte("hello")
		msg.Header.Set("Content-Type", "text/plain")
		if err := nc.PublishMsg(msg); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Subject string
			Header  nats.Header
		}{msg.Subject, msg.Header})
	})
	mux.HandleFunc("/consume", func(w http.ResponseWriter, r *http.Request) {
		select {
		case m := <-received:
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, "received subject=%s value=%s traceparent=%s\n",
				m.Subject, string(m.Data), m.Header.Get("traceparent"))
		case <-time.After(5 * time.Second):
			http.Error(w, "timed out waiting for message", http.StatusGatewayTimeout)
		}
	})

	server := &http.Server{Addr: fmt.Sprintf(":%d", *frontPort), Handler: mux}
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("frontend server: %v", err)
		}
	}()
	log.Printf("listening on :%d", *frontPort)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}
