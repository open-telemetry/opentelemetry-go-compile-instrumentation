// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"log"
	"os"

	"github.com/sirupsen/logrus"
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

	logger := logrus.New()
	logger.SetFormatter(&logrus.JSONFormatter{})
	logger.Info("logrus info message from New logger")
	logger.WithField("key", "value").Info("logrus info with field")

	logrus.Info("logrus standard info message")
	logrus.WithField("standard_key", "standard_value").Info("logrus standard info with field")

	entry := logger.WithField("entry_key", "entry_value")
	entry.Info("logrus entry info message")

	logger.WithContext(ctx).Info("logrus info message with context")

	span.End()
}
