// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a minimal segmentio/kafka-go producer for integration
// testing against a real broker.
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

var topic = flag.String("topic", "orders", "kafka topic")

func brokers() []string {
	if v := os.Getenv("KAFKA_BROKERS"); v != "" {
		return strings.Split(v, ",")
	}
	return []string{"localhost:9092"}
}

func main() {
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	writeMessage(ctx, "order-1", "hello kafka")
	slog.Info("produced message", "topic", *topic)
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
