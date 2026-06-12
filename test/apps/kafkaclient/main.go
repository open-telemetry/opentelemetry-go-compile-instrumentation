// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a minimal Kafka client for integration testing.
// This client is designed to be instrumented with the otel compile-time tool.
package main

import (
	"context"
	"log"
	"log/slog"
	"time"

	kafka "github.com/segmentio/kafka-go"
)

func main() {
	topic := "test-topic"
	broker := "localhost:9092"
	addr := "localhost:9092"
	ctx := context.Background()
	w := kafka.Writer{
		Addr:     kafka.TCP(addr),
		Topic:    topic,
		Balancer: &kafka.Hash{},
	}

	defer w.Close()

	err := w.WriteMessages(ctx, kafka.Message{
		Key:   []byte("test-key"),
		Value: []byte("test-value"),
	})
	if err != nil {
		log.Fatal("failed to write message", err)
	}

	slog.Info("WRITE", "key", "test-key", "value", "test-value")
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  []string{broker},
		Topic:    topic,
		GroupID:  "test-group",
		MinBytes: 1,
		MaxBytes: 10e6,
		MaxWait:  1 * time.Second,
	})

	defer r.Close()

	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	msg, err := r.ReadMessage(readCtx)
	if err != nil {
		log.Fatal("failed to read message", err)
	}

	slog.Info("READ", "key", string(msg.Key), "value", string(msg.Value), "topic", msg.Topic)
}
