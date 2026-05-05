// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package otlpfilter

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsExporterPath(t *testing.T) {
	tests := []struct {
		name       string
		fullMethod string
		want       bool
	}{
		{
			name:       "trace exporter",
			fullMethod: "/opentelemetry.proto.collector.trace.v1.TraceService/Export",
			want:       true,
		},
		{
			name:       "metrics exporter",
			fullMethod: "/opentelemetry.proto.collector.metrics.v1.MetricsService/Export",
			want:       true,
		},
		{
			name:       "logs exporter",
			fullMethod: "/opentelemetry.proto.collector.logs.v1.LogsService/Export",
			want:       true,
		},
		{
			name:       "regular RPC",
			fullMethod: "/grpc.testing.TestService/UnaryCall",
			want:       false,
		},
		{
			name:       "empty method",
			fullMethod: "",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsExporterPath(tt.fullMethod))
		})
	}
}

func TestIsExporterTarget(t *testing.T) {
	tests := []struct {
		name   string
		envVar string
		target string
		want   bool
	}{
		{
			name:   "global endpoint",
			envVar: "OTEL_EXPORTER_OTLP_ENDPOINT",
			target: "localhost:4317",
			want:   true,
		},
		{
			name:   "traces endpoint",
			envVar: "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
			target: "localhost:4317",
			want:   true,
		},
		{
			name:   "metrics endpoint",
			envVar: "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
			target: "localhost:4317",
			want:   true,
		},
		{
			name:   "logs endpoint",
			envVar: "OTEL_EXPORTER_OTLP_LOGS_ENDPOINT",
			target: "localhost:4317",
			want:   true,
		},
		{
			name:   "empty target",
			envVar: "OTEL_EXPORTER_OTLP_ENDPOINT",
			target: "",
			want:   false,
		},
		{
			name:   "target does not match",
			envVar: "OTEL_EXPORTER_OTLP_ENDPOINT",
			target: "localhost:50051",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.envVar, "http://localhost:4317")

			assert.Equal(t, tt.want, IsExporterTarget(tt.target))
		})
	}
}

func TestIsExporterTargetChecksAllEndpoints(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:50051")
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "http://localhost:4317")

	assert.True(t, IsExporterTarget("localhost:4317"))
}
