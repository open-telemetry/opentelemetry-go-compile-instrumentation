// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package shared

import (
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/otelsetup"
)

var setupOnce sync.Once

func init() {
	// Initialize OTel SDK when this package is first imported
	// This ensures SDK is ready before other packages build their instrumenters
	_ = SetupOTelSDK()
}

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

// Instrumented checks if instrumentation is enabled via environment variables.
//
// Environment variables (following OTel JS pattern):
//   - OTEL_GO_ENABLED_INSTRUMENTATIONS: comma-separated list of enabled instrumentations (e.g., "nethttp,grpc")
//   - OTEL_GO_DISABLED_INSTRUMENTATIONS: comma-separated list of disabled instrumentations (e.g., "nethttp")
//
// Logic:
//  1. If OTEL_GO_ENABLED_INSTRUMENTATIONS is set, only those instrumentations are enabled
//  2. Then OTEL_GO_DISABLED_INSTRUMENTATIONS is applied to disable specific ones
//  3. If neither is set, all instrumentations are enabled by default
//
// The instrumentationName should be lowercase (e.g., "nethttp", "grpc").
func Instrumented(instrumentationName string) bool {
	name := strings.ToLower(instrumentationName)

	// Check if specific instrumentations are enabled
	enabledList := os.Getenv("OTEL_GO_ENABLED_INSTRUMENTATIONS")
	if enabledList != "" {
		enabled := parseInstrumentationList(enabledList)
		if !contains(enabled, name) {
			return false
		}
	}

	// Check if this instrumentation is explicitly disabled
	disabledList := os.Getenv("OTEL_GO_DISABLED_INSTRUMENTATIONS")
	if disabledList != "" {
		disabled := parseInstrumentationList(disabledList)
		if contains(disabled, name) {
			return false
		}
	}

	return true
}

// parseInstrumentationList parses a comma-separated list of instrumentation names.
func parseInstrumentationList(list string) []string {
	var result []string
	for _, item := range strings.Split(list, ",") {
		trimmed := strings.TrimSpace(strings.ToLower(item))
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// contains checks if a slice contains a string.
func contains(slice []string, str string) bool {
	for _, s := range slice {
		if s == str {
			return true
		}
	}
	return false
}
