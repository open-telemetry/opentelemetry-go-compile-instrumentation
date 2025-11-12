// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package shared

import (
	"log/slog"
	"os"
	"sync"

	"go.opentelemetry.io/otel"
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
func SetupOTelSDK() error {
	setupOnce.Do(func() {
		log := GetLogger()

		// Setup stdout exporter for debugging
		// In production, applications should configure their own exporters
		// This will only run once due to sync.Once
		exporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			setupErr = err
			log.Error("failed to create stdout trace exporter", "error", err)
			return
		}

		tp := trace.NewTracerProvider(
			trace.WithBatcher(exporter),
		)
		otel.SetTracerProvider(tp)

		log.Info("OTel SDK initialized with stdout exporter",
			"provider", "TracerProvider",
			"exporter", "stdout")
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
