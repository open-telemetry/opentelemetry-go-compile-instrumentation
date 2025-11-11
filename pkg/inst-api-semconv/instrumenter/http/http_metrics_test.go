// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
//

package http

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst-api-semconv/instrumenter/utils"
)

// Test helper functions
func newHTTPServerMetric(key string, meter metric.Meter) (*HTTPServerMetric, error) {
	registry := NewMetricsRegistry(slog.Default(), meter)
	return registry.NewHTTPServerMetric(key)
}

func newHTTPClientMetric(key string, meter metric.Meter) (*HTTPClientMetric, error) {
	registry := NewMetricsRegistry(slog.Default(), meter)
	return registry.NewHTTPClientMetric(key)
}

func TestHTTPServerMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName("my-service"),
		semconv.ServiceVersion("v0.1.0"),
	)
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithResource(res), sdkmetric.WithReader(reader))
	meter := mp.Meter("test-meter")
	server, err := newHTTPServerMetric("test", meter)
	require.NoError(t, err)
	ctx := context.Background()
	start := time.Now()
	ctx = server.OnBeforeStart(ctx, start)
	ctx = server.OnBeforeEnd(ctx, []attribute.KeyValue{}, start)
	server.OnAfterStart(ctx, start)
	server.OnAfterEnd(ctx, []attribute.KeyValue{}, time.Now())
	rm := &metricdata.ResourceMetrics{}
	_ = reader.Collect(ctx, rm)
	assert.Equal(t, "http.server.request.duration", rm.ScopeMetrics[0].Metrics[0].Name)
}

func TestHTTPClientMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName("my-service"),
		semconv.ServiceVersion("v0.1.0"),
	)
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithResource(res), sdkmetric.WithReader(reader))
	meter := mp.Meter("test-meter")
	client, err := newHTTPClientMetric("test", meter)
	require.NoError(t, err)
	ctx := context.Background()
	start := time.Now()
	ctx = client.OnBeforeStart(ctx, start)
	ctx = client.OnBeforeEnd(ctx, []attribute.KeyValue{}, start)
	client.OnAfterStart(ctx, start)
	client.OnAfterEnd(ctx, []attribute.KeyValue{}, time.Now())
	rm := &metricdata.ResourceMetrics{}
	_ = reader.Collect(ctx, rm)
	assert.Equal(t, "http.client.request.duration", rm.ScopeMetrics[0].Metrics[0].Name)
}

func TestHTTPMetricAttributesShadower(t *testing.T) {
	attrs := make([]attribute.KeyValue, 0)
	attrs = append(attrs, attribute.KeyValue{
		Key:   semconv.HTTPRequestMethodKey,
		Value: attribute.StringValue("method"),
	}, attribute.KeyValue{
		Key:   "unknown",
		Value: attribute.Value{},
	}, attribute.KeyValue{
		Key:   semconv.NetworkProtocolNameKey,
		Value: attribute.StringValue("http"),
	}, attribute.KeyValue{
		Key:   semconv.ServerPortKey,
		Value: attribute.IntValue(8080),
	})
	n, attrs := utils.Shadow(attrs, httpMetricsConv)
	assert.Equal(t, 3, n)
	assert.Equal(t, attribute.Key("unknown"), attrs[n].Key)
}

// Tests for MetricsRegistry API
func TestMetricsRegistryHTTPServerMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName("my-service"),
		semconv.ServiceVersion("v0.1.0"),
	)
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithResource(res), sdkmetric.WithReader(reader))
	meter := mp.Meter("test-meter")

	registry := NewMetricsRegistry(slog.Default(), meter)
	server, err := registry.NewHTTPServerMetric("test")
	require.NoError(t, err)

	ctx := context.Background()
	start := time.Now()
	ctx = server.OnBeforeStart(ctx, start)
	ctx = server.OnBeforeEnd(ctx, []attribute.KeyValue{}, start)
	server.OnAfterStart(ctx, start)
	server.OnAfterEnd(ctx, []attribute.KeyValue{}, time.Now())

	rm := &metricdata.ResourceMetrics{}
	_ = reader.Collect(ctx, rm)
	assert.Equal(t, "http.server.request.duration", rm.ScopeMetrics[0].Metrics[0].Name)
}

func TestMetricsRegistryHTTPClientMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName("my-service"),
		semconv.ServiceVersion("v0.1.0"),
	)
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithResource(res), sdkmetric.WithReader(reader))
	meter := mp.Meter("test-meter")

	registry := NewMetricsRegistry(slog.Default(), meter)
	client, err := registry.NewHTTPClientMetric("test")
	require.NoError(t, err)

	ctx := context.Background()
	start := time.Now()
	ctx = client.OnBeforeStart(ctx, start)
	ctx = client.OnBeforeEnd(ctx, []attribute.KeyValue{}, start)
	client.OnAfterStart(ctx, start)
	client.OnAfterEnd(ctx, []attribute.KeyValue{}, time.Now())

	rm := &metricdata.ResourceMetrics{}
	_ = reader.Collect(ctx, rm)
	assert.Equal(t, "http.client.request.duration", rm.ScopeMetrics[0].Metrics[0].Name)
}

func TestClientNilMeter(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName("my-service"),
		semconv.ServiceVersion("v0.1.0"),
	)
	_ = sdkmetric.NewMeterProvider(sdkmetric.WithResource(res), sdkmetric.WithReader(reader))
	_, err := newHTTPClientMetric("test", nil)
	require.Error(t, err, "expected error for nil meter, but got nil")
}

func TestServerNilMeter(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName("my-service"),
		semconv.ServiceVersion("v0.1.0"),
	)
	_ = sdkmetric.NewMeterProvider(sdkmetric.WithResource(res), sdkmetric.WithReader(reader))
	_, err := newHTTPServerMetric("test", nil)
	require.Error(t, err, "expected error for nil meter, but got nil")
}

// Tests for NoopRegistry
func TestNoopRegistry(t *testing.T) {
	registry := NewNoOpRegistry()

	// Test creating server metric
	server, err := registry.NewHTTPServerMetric("test.server")
	require.NoError(t, err, "expected no error creating no-op server metric")
	require.NotNil(t, server, "expected noop server metric")
	assert.Equal(t, attribute.Key("test.server"), server.key, "expected key 'test.server'")

	// Test creating client metric
	client, err := registry.NewHTTPClientMetric("test.client")
	require.NoError(t, err, "expected no error creating no-op client metric")
	require.NotNil(t, client, "expected noop client metric")
	assert.Equal(t, attribute.Key("test.client"), client.key, "expected key 'test.client'")
}

func TestNoopMetricsDoNotPanic(_ *testing.T) {
	registry := NewNoOpRegistry()

	// Create no-op metrics
	server, _ := registry.NewHTTPServerMetric("test.server")
	client, _ := registry.NewHTTPClientMetric("test.client")

	// Test that all methods can be called without panicking
	ctx := context.Background()
	start := time.Now()

	// Test server metric methods
	ctx = server.OnBeforeStart(ctx, start)
	ctx = server.OnBeforeEnd(ctx, []attribute.KeyValue{}, start)
	server.OnAfterStart(ctx, start)
	server.OnAfterEnd(ctx, []attribute.KeyValue{}, time.Now())

	// Test client metric methods
	ctx = client.OnBeforeStart(ctx, start)
	ctx = client.OnBeforeEnd(ctx, []attribute.KeyValue{}, start)
	client.OnAfterStart(ctx, start)
	client.OnAfterEnd(ctx, []attribute.KeyValue{}, time.Now())

	// If we get here without panicking, the test passes
}

func TestRegistryInterface(t *testing.T) {
	// Test that both implementations satisfy the Registry interface
	var _ Registry = (*MetricsRegistry)(nil)
	var _ Registry = (*NoopRegistry)(nil)

	// Test using the interface
	testRegistry := func(r Registry) {
		server, err := r.NewHTTPServerMetric("test")
		if err != nil && r.(*MetricsRegistry) != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if server == nil {
			t.Fatal("expected non-nil server metric")
		}

		client, err := r.NewHTTPClientMetric("test")
		if err != nil && r.(*MetricsRegistry) != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client metric")
		}
	}

	// Test with NoOpRegistry
	testRegistry(NewNoOpRegistry())
}
