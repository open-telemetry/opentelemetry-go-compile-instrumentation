// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package shared

import (
	"log/slog"
	"os"
	"sync"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/otelsetup"
)

var (
	setupOnce sync.Once
)

// GetLogger returns a shared logger instance for instrumentation
// It uses OTEL_LOG_LEVEL environment variable (debug, info, warn, error)
func GetLogger() *slog.Logger {
	return otelsetup.GetLogger()
}

// SetupOTelSDK initializes the OpenTelemetry SDK if not already initialized
// This function is idempotent and safe to call multiple times
// Returns error only on first initialization failure
//
// The SDK automatically configures exporters based on environment variables:
// - OTEL_EXPORTER_OTLP_ENDPOINT: OTLP endpoint (e.g., http://localhost:4317)
// - OTEL_EXPORTER_OTLP_TRACES_ENDPOINT: Traces-specific endpoint
// - OTEL_EXPORTER_OTLP_METRICS_ENDPOINT: Metrics-specific endpoint
// - OTEL_SERVICE_NAME: Service name for telemetry
// - OTEL_LOG_LEVEL: Log level (debug, info, warn, error)
func SetupOTelSDK() error {
	setupOnce.Do(func() {
		// Initialize OpenTelemetry SDK with defensive error handling
		otelsetup.Initialize(otelsetup.Config{
			ServiceName:            "otel-instrumentation",
			ServiceVersion:         "0.1.0",
			InstrumentationName:    "github.com/open-telemetry/opentelemetry-go-compile-instrumentation",
			InstrumentationVersion: "0.1.0",
		})
	})
	return nil
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
