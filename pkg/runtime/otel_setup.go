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
// SDK Configuration:
//   - OTEL_SDK_DISABLED: If set to the case-insensitive string "true", the SDK
//     is disabled entirely and no providers are installed. Every other value
//     (including unset) leaves the SDK enabled, per the OpenTelemetry
//     specification.
//
// Service Configuration (highest to lowest precedence):
//   - OTEL_RESOURCE_ATTRIBUTES: Key-value pairs (e.g., "service.name=myapp,service.version=1.2.3")
//   - OTEL_SERVICE_NAME: Service name for telemetry
//
// Exporter Configuration (applies independently to each signal; traces,
// metrics, and logs are all enabled by default and configured symmetrically):
//   - OTEL_TRACES_EXPORTER: Traces exporter: otlp (default), console, none
//   - OTEL_METRICS_EXPORTER: Metrics exporter: otlp (default), console, prometheus, none
//   - OTEL_LOGS_EXPORTER: Logs exporter: otlp (default), console, none
//   - OTEL_EXPORTER_OTLP_ENDPOINT: OTLP endpoint for all signals (default: http://localhost:4318)
//   - OTEL_EXPORTER_OTLP_TRACES_ENDPOINT: Traces-specific endpoint override
//   - OTEL_EXPORTER_OTLP_METRICS_ENDPOINT: Metrics-specific endpoint override
//   - OTEL_EXPORTER_OTLP_LOGS_ENDPOINT: Logs-specific endpoint override
//   - OTEL_EXPORTER_OTLP_PROTOCOL: Protocol (grpc, http/protobuf, http/json)
//   - OTEL_EXPORTER_PROMETHEUS_HOST: Prometheus exporter host (default: localhost)
//   - OTEL_EXPORTER_PROMETHEUS_PORT: Prometheus exporter port (default: 9464)
//
// When the exporter for a signal defaults to "otlp" and no endpoint override
// is set, telemetry for that signal is sent to the OTLP-spec default endpoint
// (http://localhost:4318). This means traces, metrics, and logs are exported
// by default even when no OTLP endpoint is configured; set the relevant
// *_EXPORTER variable to "none" to disable a signal explicitly.
//
// Other Configuration:
//   - OTEL_LOG_LEVEL: Log level (debug, info, warn, error)
func SetupOTelSDK() {
	if strings.EqualFold(os.Getenv("OTEL_SDK_DISABLED"), "true") {
		Logger().Info("OpenTelemetry SDK disabled via OTEL_SDK_DISABLED=true, skipping initialization")
		return
	}

	// Initialize OpenTelemetry SDK with defensive error handling
	Initialize(Config{
		InstrumentationName:    "go.opentelemetry.io/otelc",
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
