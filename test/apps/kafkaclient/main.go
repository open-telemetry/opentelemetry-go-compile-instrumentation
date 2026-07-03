// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a minimal segmentio/kafka-go client for integration
// testing against a real broker (started as a testcontainer). The broker
// address is read from the KAFKA_BROKERS environment variable.
// This client is designed to be instrumented with the otelc compile-time tool.
package main

import (
	"context"
	"flag"
	"log"
	"log/slog"
	"os"
	"strings"
	"time"

	kafka "github.com/segmentio/kafka-go"
)

var (
	op    = flag.String("op", "produce", "operation: produce | consume")
	topic = flag.String("topic", "orders", "kafka topic")
)

func brokers() []string {
	if v := os.Getenv("KAFKA_BROKERS"); v != "" {
		return strings.Split(v, ",")
	}
	return []string{"localhost:9092"}
}

func main() {
	flag.Parse()

	switch *op {
	case "produce":
		doProduce()
	case "consume":
		doConsume()
	default:
		log.Fatalf("unknown operation: %s", *op)
	}
}

// ensureTopic creates the topic up front (best-effort) so a single
// WriteMessages call succeeds and emits exactly one producer span. CreateTopics
// is not instrumented, so it adds no spans of its own.
func ensureTopic(ctx context.Context) {
	client := &kafka.Client{Addr: kafka.TCP(brokers()...)}
	_, _ = client.CreateTopics(ctx, &kafka.CreateTopicsRequest{
		Topics: []kafka.TopicConfig{{
			Topic:             *topic,
			NumPartitions:     1,
			ReplicationFactor: 1,
		}},
	})
}

// writeMessage sends a single message. The Writer retries transient errors
// (e.g. leader election after topic creation) internally, so the whole send is
// one instrumented WriteMessages call.
func writeMessage(ctx context.Context, key, value string) {
	ensureTopic(ctx)

	w := &kafka.Writer{
		Addr:         kafka.TCP(brokers()...),
		Topic:        *topic,
		Balancer:     &kafka.LeastBytes{},
		BatchSize:    1,
		BatchTimeout: 10 * time.Millisecond,
		RequiredAcks: kafka.RequireAll,
	}
	defer w.Close()

	if err := w.WriteMessages(ctx, kafka.Message{Key: []byte(key), Value: []byte(value)}); err != nil {
		log.Fatalf("failed to write messages: %v", err)
	}
}

func doProduce() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	writeMessage(ctx, "order-1", "hello kafka")
	slog.Info("produced message", "topic", *topic)
}

func doConsume() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Seed a message so the reader has something to consume. The instrumented
	// writer injects trace context into the message headers, which the reader
	// then extracts — exercising context propagation across the two hooks.
	writeMessage(ctx, "order-1", "hello kafka")

	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     brokers(),
		Topic:       *topic,
		Partition:   0,
		StartOffset: kafka.FirstOffset,
		MaxWait:     500 * time.Millisecond,
	})
	defer r.Close()

	msg, err := r.ReadMessage(ctx)
	if err != nil {
		log.Fatalf("failed to read message: %v", err)
	}
	slog.Info("consumed message", "topic", *topic, "key", string(msg.Key), "offset", msg.Offset)
}
