// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"os"
	"slices"
	"strings"
)

// SetupOTelSDK initializes the OpenTelemetry SDK.
//
// The SDK automatically configures exporters based on environment variables
// following the OpenTelemetry specification:
//
// Service Configuration (highest to lowest precedence):
//   - OTEL_RESOURCE_ATTRIBUTES: Key-value pairs (e.g., "service.name=myapp,service.version=1.2.3")
//   - OTEL_SERVICE_NAME: Service name for telemetry
//
// Exporter Configuration:
//   - OTEL_EXPORTER_OTLP_ENDPOINT: OTLP endpoint (e.g., http://localhost:4317)
//   - OTEL_EXPORTER_OTLP_TRACES_ENDPOINT: Traces-specific endpoint
//   - OTEL_EXPORTER_OTLP_METRICS_ENDPOINT: Metrics-specific endpoint
//   - OTEL_EXPORTER_OTLP_PROTOCOL: Protocol (grpc, http/protobuf, http/json)
//   - OTEL_TRACES_EXPORTER: Trace exporter (otlp, console, none)
//   - OTEL_METRICS_EXPORTER: Metrics exporter (otlp, console, none)
//
// Other Configuration:
//   - OTEL_LOG_LEVEL: Log level (debug, info, warn, error)
//   - OTEL_SDK_DISABLED: Disable the SDK (true/false)
//
// Example usage from an instrumentation:
//
//	version := instrumentationVersion()
//	if err := runtime.SetupOTelSDK("go.opentelemetry.io/compile-instrumentation/google.golang.org/grpc/client", version); err != nil {
//	    logger.Error("failed to setup OTel SDK", "error", err)
//	}
func SetupOTelSDK() {
	// Initialize OpenTelemetry SDK with defensive error handling
	Initialize(Config{
		InstrumentationName:    "go.opentelemetry.io/compile-instrumentation",
		InstrumentationVersion: ModuleVersion(),
	})
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
		if !slices.Contains(enabled, name) {
			return false
		}
	}

	// Check if this instrumentation is explicitly disabled
	disabledList := os.Getenv("OTEL_GO_DISABLED_INSTRUMENTATIONS")
	if disabledList != "" {
		disabled := parseInstrumentationList(disabledList)
		if slices.Contains(disabled, name) {
			return false
		}
	}

	return true
}

// parseInstrumentationList parses a comma-separated list of instrumentation names.
func parseInstrumentationList(list string) []string {
	var result []string
	for item := range strings.SplitSeq(list, ",") {
		trimmed := strings.TrimSpace(strings.ToLower(item))
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
