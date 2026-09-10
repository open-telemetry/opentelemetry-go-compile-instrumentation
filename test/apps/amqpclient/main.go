// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main publishes one RabbitMQ message and consumes it with a manual
// ack so integration tests see one send span and one process span.
package main

import (
	"context"
	"flag"
	"log"
	"log/slog"
	"os"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

func main() {
	queue := flag.String("queue", "orders", "queue name")
	url := flag.String("amqp-url", envOr("AMQP_URL", "amqp://guest:guest@localhost:5672/"), "AMQP URL")
	flag.Parse()

	conn, err := amqp.Dial(*url)
	if err != nil {
		log.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("channel: %v", err)
	}
	defer ch.Close()

	q, err := ch.QueueDeclare(*queue, false, true, false, false, nil)
	if err != nil {
		log.Fatalf("declare: %v", err)
	}

	deliveries, err := ch.Consume(q.Name, "", false, false, false, false, nil)
	if err != nil {
		log.Fatalf("consume: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = ch.PublishWithContext(ctx, "", q.Name, false, false, amqp.Publishing{
		ContentType: "text/plain",
		Body:        []byte("hello amqp"),
	})
	if err != nil {
		log.Fatalf("publish: %v", err)
	}
	slog.Info("published message", "queue", q.Name)

	select {
	case d := <-deliveries:
		if err := d.Ack(false); err != nil {
			log.Fatalf("ack: %v", err)
		}
		slog.Info("consumed message", "queue", q.Name)
	case <-ctx.Done():
		log.Fatalf("consume: %v", ctx.Err())
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
