// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main is a complex demo app combining HTTP server and Kafka producer for e2e testing.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	kafka "github.com/segmentio/kafka-go"
)

var frontPort = flag.Int("front-port", 8080, "port for HTTP frontend")

func brokers() []string {
	if v := os.Getenv("KAFKA_BROKERS"); v != "" {
		return strings.Split(v, ",")
	}
	return []string{"localhost:9092"}
}

// ensureTopic creates the topic up front (best-effort) so a single WriteMessages
// call succeeds and emits exactly one producer span. CreateTopics is not instrumented.
func ensureTopic(ctx context.Context, topic string) {
	client := &kafka.Client{Addr: kafka.TCP(brokers()...)}
	_, _ = client.CreateTopics(ctx, &kafka.CreateTopicsRequest{
		Topics: []kafka.TopicConfig{{
			Topic:             topic,
			NumPartitions:     1,
			ReplicationFactor: 1,
		}},
	})
}

func main() {
	flag.Parse()

	topic := "test-topic-http"

	produce := func(w http.ResponseWriter, r *http.Request) {
		ensureTopic(r.Context(), topic)

		writer := &kafka.Writer{
			Addr:         kafka.TCP(brokers()...),
			Topic:        topic,
			Balancer:     &kafka.LeastBytes{},
			BatchSize:    1,
			BatchTimeout: 10 * time.Millisecond,
			RequiredAcks: kafka.RequireAll,
		}
		defer writer.Close()

		err := writer.WriteMessages(r.Context(), kafka.Message{
			Key:   []byte("test-key"),
			Value: []byte("test-message"),
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("frontend produced message to kafka"))
	}

	mux := http.NewServeMux()
	// /produce is kept for the existing integration test; /hello lets the
	// e2e test reuse test/apps/httpclient, which always requests /hello.
	mux.HandleFunc("/produce", produce)
	mux.HandleFunc("/hello", produce)

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
