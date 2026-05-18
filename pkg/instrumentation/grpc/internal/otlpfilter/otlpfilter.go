// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package otlpfilter identifies OTLP exporter gRPC calls that must not be
// instrumented by the gRPC instrumentation itself.
package otlpfilter

import (
	"os"
	"strings"
)

const (
	exporterTracePath   = "/opentelemetry.proto.collector.trace.v1.TraceService/Export"
	exporterMetricsPath = "/opentelemetry.proto.collector.metrics.v1.MetricsService/Export"
	exporterLogsPath    = "/opentelemetry.proto.collector.logs.v1.LogsService/Export"
)

var exporterEndpointEnvVars = []string{
	"OTEL_EXPORTER_OTLP_ENDPOINT",
	"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
	"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
	"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT",
}

// IsExporterPath returns true when fullMethod is an OTLP exporter method.
func IsExporterPath(fullMethod string) bool {
	return fullMethod == exporterTracePath ||
		fullMethod == exporterMetricsPath ||
		fullMethod == exporterLogsPath
}

// IsExporterTarget returns true when target matches a configured OTLP exporter endpoint.
func IsExporterTarget(target string) bool {
	if target == "" {
		return false
	}

	for _, envVar := range exporterEndpointEnvVars {
		endpoint := os.Getenv(envVar)
		if endpoint != "" && strings.Contains(endpoint, target) {
			return true
		}
	}
	return false
}
