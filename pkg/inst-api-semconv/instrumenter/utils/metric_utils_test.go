// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"
)

func TestNewFloat64Histogram(t *testing.T) {
	reader := metric.NewManualReader()
	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName("my-service"),
		semconv.ServiceVersion("v0.1.0"),
	)
	mp := metric.NewMeterProvider(metric.WithResource(res), metric.WithReader(reader))
	meter := mp.Meter("test-meter")
	server, err := NewFloat64Histogram("test", "ms", "test metric", meter)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	server.Record(ctx, 1.0)
	rm := &metricdata.ResourceMetrics{}
	_ = reader.Collect(ctx, rm)
	if rm.ScopeMetrics[0].Metrics[0].Name != "test" {
		t.Fatal("wrong metrics name, " + rm.ScopeMetrics[0].Metrics[0].Name)
	}
}
