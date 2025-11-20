// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rpc

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"
)

func TestRpcServerMetrics(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	meter := provider.Meter("test")

	serverMetric := RpcServerMetrics("rpc.server")
	serverMetric.serverRequestDuration, _ = meter.Float64Histogram(
		rpcServerRequestDuration,
	)

	// Test OnBeforeStart
	ctx := context.Background()
	startTime := time.Now()
	ctx = serverMetric.OnBeforeStart(ctx, startTime)
	require.NotNil(t, ctx)

	// Test OnAfterStart
	serverMetric.OnAfterStart(ctx, time.Now())

	// Test OnBeforeEnd
	startAttrs := []attribute.KeyValue{
		{Key: semconv.RPCSystemKey, Value: attribute.StringValue("grpc")},
		{Key: semconv.RPCServiceKey, Value: attribute.StringValue("/helloworld.Greeter")},
		{Key: semconv.RPCMethodKey, Value: attribute.StringValue("SayHello")},
	}
	ctx = serverMetric.OnBeforeEnd(ctx, startAttrs, startTime)
	require.NotNil(t, ctx)

	// Test OnAfterEnd
	endAttrs := []attribute.KeyValue{
		{Key: semconv.RPCGRPCStatusCodeKey, Value: attribute.IntValue(0)},
		{Key: semconv.ServerAddressKey, Value: attribute.StringValue("localhost:50051")},
	}
	time.Sleep(10 * time.Millisecond) // Ensure some duration
	serverMetric.OnAfterEnd(ctx, endAttrs, time.Now())

	// Verify metrics were recorded
	var rm metricdata.ResourceMetrics
	err := reader.Collect(ctx, &rm)
	require.NoError(t, err)
	require.Len(t, rm.ScopeMetrics, 1)
	require.Len(t, rm.ScopeMetrics[0].Metrics, 1)

	histogram := rm.ScopeMetrics[0].Metrics[0]
	require.Equal(t, rpcServerRequestDuration, histogram.Name)
}

func TestRpcClientMetrics(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	meter := provider.Meter("test")

	clientMetric := RpcClientMetrics("rpc.client")
	clientMetric.clientRequestDuration, _ = meter.Float64Histogram(
		rpcClientRequestDuration,
	)

	// Test OnBeforeStart
	ctx := context.Background()
	startTime := time.Now()
	ctx = clientMetric.OnBeforeStart(ctx, startTime)
	require.NotNil(t, ctx)

	// Test OnAfterStart
	clientMetric.OnAfterStart(ctx, time.Now())

	// Test OnBeforeEnd
	startAttrs := []attribute.KeyValue{
		{Key: semconv.RPCSystemKey, Value: attribute.StringValue("grpc")},
		{Key: semconv.RPCServiceKey, Value: attribute.StringValue("/helloworld.Greeter")},
		{Key: semconv.RPCMethodKey, Value: attribute.StringValue("SayHello")},
	}
	ctx = clientMetric.OnBeforeEnd(ctx, startAttrs, startTime)
	require.NotNil(t, ctx)

	// Test OnAfterEnd
	endAttrs := []attribute.KeyValue{
		{Key: semconv.RPCGRPCStatusCodeKey, Value: attribute.IntValue(0)},
		{Key: semconv.ServerAddressKey, Value: attribute.StringValue("localhost:50051")},
	}
	time.Sleep(10 * time.Millisecond) // Ensure some duration
	clientMetric.OnAfterEnd(ctx, endAttrs, time.Now())

	// Verify metrics were recorded
	var rm metricdata.ResourceMetrics
	err := reader.Collect(ctx, &rm)
	require.NoError(t, err)
	require.Len(t, rm.ScopeMetrics, 1)
	require.Len(t, rm.ScopeMetrics[0].Metrics, 1)

	histogram := rm.ScopeMetrics[0].Metrics[0]
	require.Equal(t, rpcClientRequestDuration, histogram.Name)
}

func TestRpcMetricsAttributeFiltering(t *testing.T) {
	// Test that only relevant attributes are included in metrics
	allAttrs := []attribute.KeyValue{
		{Key: semconv.RPCSystemKey, Value: attribute.StringValue("grpc")},
		{Key: semconv.RPCServiceKey, Value: attribute.StringValue("/helloworld.Greeter")},
		{Key: semconv.RPCMethodKey, Value: attribute.StringValue("SayHello")},
		{Key: semconv.ServerAddressKey, Value: attribute.StringValue("localhost:50051")},
		{Key: semconv.RPCGRPCStatusCodeKey, Value: attribute.IntValue(0)},
		{Key: attribute.Key("some.other.attribute"), Value: attribute.StringValue("value")}, // Should be filtered out
	}

	n, filtered := shadowRpcMetricsAttrs(allAttrs)

	// Check that only the expected attributes are included
	require.Equal(t, 5, n)
	require.GreaterOrEqual(t, len(filtered), 5)

	expectedKeys := map[attribute.Key]bool{
		semconv.RPCSystemKey:         true,
		semconv.RPCServiceKey:        true,
		semconv.RPCMethodKey:         true,
		semconv.ServerAddressKey:     true,
		semconv.RPCGRPCStatusCodeKey: true,
	}

	for i := 0; i < n; i++ {
		require.True(t, expectedKeys[filtered[i].Key], "unexpected key: %s", filtered[i].Key)
	}
}
