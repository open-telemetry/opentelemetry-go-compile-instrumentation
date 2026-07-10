// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"log"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func main() {
	exporter, err := stdouttrace.New(stdouttrace.WithWriter(os.Stdout))
	if err != nil {
		log.Fatalf("failed to create exporter: %v", err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)),
	)
	otel.SetTracerProvider(tp)

	tracer := otel.GetTracerProvider().Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	slog.InfoContext(ctx, "slog info message with context")
	slog.WarnContext(ctx, "slog warn message with context")
	slog.ErrorContext(ctx, "slog error message with context")

	slog.Info("slog info message without context")
	slog.Warn("slog warn message without context")

	log.Println("standard log message 1")
	log.Printf("standard log message 2 with format")
	log.Print("standard log message 3")

	span.End()
}
