// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package shared

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/trace"
)

var (
	setupOnce  sync.Once
	logger     *slog.Logger
	loggerOnce sync.Once
	setupErr   error
)

// GetLogger returns a shared logger instance for instrumentation
// It uses OTEL_LOG_LEVEL environment variable (debug, info, warn, error)
func GetLogger() *slog.Logger {
	loggerOnce.Do(func() {
		var level slog.Level
		logLevel := os.Getenv("OTEL_LOG_LEVEL")
		switch logLevel {
		case "debug":
			level = slog.LevelDebug
		case "info":
			level = slog.LevelInfo
		case "warn":
			level = slog.LevelWarn
		case "error":
			level = slog.LevelError
		default:
			level = slog.LevelInfo
		}

		opts := &slog.HandlerOptions{Level: level}
		logger = slog.New(slog.NewJSONHandler(os.Stdout, opts))
	})
	return logger
}

// SetupOTelSDK initializes the OpenTelemetry SDK if not already initialized
// This function is idempotent and safe to call multiple times
// Returns error only on first initialization failure
//
// The SDK automatically configures exporters based on environment variables:
// - OTEL_EXPORTER_OTLP_ENDPOINT: OTLP endpoint (e.g., http://localhost:4317)
// - OTEL_EXPORTER_OTLP_PROTOCOL: Protocol (grpc or http/protobuf)
// - If no OTLP endpoint configured, uses stdout exporter for local development
func SetupOTelSDK() error {
	setupOnce.Do(func() {
		log := GetLogger()

		var exporter trace.SpanExporter
		var exporterType string

		// Check if OTLP endpoint is configured via environment variables
		otlpEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
		if otlpEndpoint != "" {
			// Use OTLP exporter (production/docker-compose)
			log.Info("Configuring OTLP exporter", "endpoint", otlpEndpoint)

			// Create OTLP exporter with non-blocking dial (connects lazily)
			// This allows the application to start even if collector isn't ready yet
			otlpExporter, err := otlptracegrpc.New(
				context.Background(),
				otlptracegrpc.WithInsecure(),
				otlptracegrpc.WithTimeout(5*time.Second),
			)
			if err != nil {
				// If OTLP exporter creation fails, fall back to stdout
				log.Warn("failed to create OTLP exporter, falling back to stdout",
					"error", err, "endpoint", otlpEndpoint)

				stdoutExporter, stdoutErr := stdouttrace.New(stdouttrace.WithPrettyPrint())
				if stdoutErr != nil {
					setupErr = stdoutErr
					log.Error("failed to create fallback stdout exporter", "error", stdoutErr)
					return
				}
				exporter = stdoutExporter
				exporterType = "stdout (fallback)"
			} else {
				exporter = otlpExporter
				exporterType = "otlp"
				log.Info("OTLP exporter configured successfully", "endpoint", otlpEndpoint)
			}
		} else {
			// Use stdout exporter for local development/testing
			log.Info("No OTLP endpoint configured, using stdout exporter")

			stdoutExporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
			if err != nil {
				setupErr = err
				log.Error("failed to create stdout trace exporter", "error", err)
				return
			}
			exporter = stdoutExporter
			exporterType = "stdout"
		}

		tp := trace.NewTracerProvider(
			trace.WithBatcher(exporter),
		)
		otel.SetTracerProvider(tp)

		log.Info("OTel SDK initialized",
			"provider", "TracerProvider",
			"exporter", exporterType,
			"endpoint", otlpEndpoint)
	})
	return setupErr
}

// IsInstrumentationEnabled checks if instrumentation is enabled via environment variable
// Default is enabled unless explicitly set to "false"
func IsInstrumentationEnabled(instrumentationName string) bool {
	// Check global flag
	if os.Getenv("OTEL_INSTRUMENTATION_ENABLED") == "false" {
		return false
	}

	// Check specific instrumentation flag (e.g., OTEL_INSTRUMENTATION_NETHTTP_ENABLED)
	envVar := "OTEL_INSTRUMENTATION_" + instrumentationName + "_ENABLED"
	return os.Getenv(envVar) != "false"
}
