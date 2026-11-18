// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main publishes one RabbitMQ message and consumes it with a manual
// ack so integration tests see one send span and one process span.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"log/slog"
	"os"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel/trace"
)

// staleTraceID is the trace of the canceled publish that -canceled-publish-first
// makes. No span of the published message may belong to it.
const staleTraceID = "0af7651916cd43dd8448eb211c80319c"

func main() {
	queue := flag.String("queue", "orders", "queue name")
	url := flag.String("amqp-url", envOr("AMQP_URL", "amqp://guest:guest@localhost:5672/"), "AMQP URL")
	autoAck := flag.Bool("auto-ack", false, "consume with auto-ack")
	canceledFirst := flag.Bool("canceled-publish-first", false,
		"call PublishWithContext with a canceled context first, then publish with Publish")
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

	deliveries, err := ch.Consume(q.Name, "", *autoAck, false, false, false, nil)
	if err != nil {
		log.Fatalf("consume: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	msg := amqp.Publishing{
		ContentType: "text/plain",
		Body:        []byte("hello amqp"),
	}
	if *canceledFirst {
		publishCanceled(ch, q.Name)
		err = ch.Publish("", q.Name, false, false, msg)
	} else {
		err = ch.PublishWithContext(ctx, "", q.Name, false, false, msg)
	}
	if err != nil {
		log.Fatalf("publish: %v", err)
	}
	slog.Info("published message", "queue", q.Name)

	select {
	case d := <-deliveries:
		if !*autoAck {
			if err := d.Ack(false); err != nil {
				log.Fatalf("ack: %v", err)
			}
		}
		slog.Info("consumed message", "queue", q.Name)
	case <-ctx.Done():
		log.Fatalf("consume: %v", ctx.Err())
	}
}

// publishCanceled calls PublishWithContext with a context that carries a span
// of staleTraceID and is already canceled, so the library returns without
// publishing.
func publishCanceled(ch *amqp.Channel, queue string) {
	traceID, err := trace.TraceIDFromHex(staleTraceID)
	if err != nil {
		log.Fatalf("trace id: %v", err)
	}
	spanID, err := trace.SpanIDFromHex("b7ad6b7169203331")
	if err != nil {
		log.Fatalf("span id: %v", err)
	}
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	ctx, cancel := context.WithCancel(trace.ContextWithRemoteSpanContext(context.Background(), sc))
	cancel()
	err = ch.PublishWithContext(ctx, "", queue, false, false, amqp.Publishing{Body: []byte("never sent")})
	if !errors.Is(err, context.Canceled) {
		log.Fatalf("canceled publish: got %v, want %v", err, context.Canceled)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
