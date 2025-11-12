// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rpc

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"
)

/**
RPC Metrics are defined following OpenTelemetry semantic conventions:
https://opentelemetry.io/docs/specs/semconv/rpc/rpc-metrics/
*/

const (
	rpcServerRequestDuration = "rpc.server.duration"
	rpcClientRequestDuration = "rpc.client.duration"
)

// rpcMetricsConv defines the attributes that should be included in RPC metrics
// This is a read-only map, so it's safe to be a package-level variable
//
//nolint:gochecknoglobals // Read-only map, safe as package-level constant
var rpcMetricsConv = map[attribute.Key]bool{
	semconv.RPCSystemKey:         true,
	semconv.RPCServiceKey:        true,
	semconv.RPCMethodKey:         true,
	semconv.ServerAddressKey:     true,
	semconv.RPCGRPCStatusCodeKey: true,
}

// RpcServerMetric implements OperationListener for RPC server metrics
type RpcServerMetric struct {
	key                   attribute.Key
	serverRequestDuration metric.Float64Histogram
	logger                *slog.Logger
}

// RpcClientMetric implements OperationListener for RPC client metrics
type RpcClientMetric struct {
	key                   attribute.Key
	clientRequestDuration metric.Float64Histogram
	logger                *slog.Logger
}

type rpcMetricContext struct {
	startTime       time.Time
	startAttributes []attribute.KeyValue
}

// RpcServerMetrics creates a new RPC server metric with the given key
func RpcServerMetrics(key string) *RpcServerMetric {
	return &RpcServerMetric{
		key:    attribute.Key(key),
		logger: slog.Default(),
	}
}

// RpcClientMetrics creates a new RPC client metric with the given key
func RpcClientMetrics(key string) *RpcClientMetric {
	return &RpcClientMetric{
		key:    attribute.Key(key),
		logger: slog.Default(),
	}
}

// OnBeforeStart is called before the operation starts
func (h *RpcServerMetric) OnBeforeStart(parentContext context.Context, startTime time.Time) context.Context {
	return parentContext
}

// OnBeforeEnd is called before the operation ends
func (h *RpcServerMetric) OnBeforeEnd(
	ctx context.Context,
	startAttributes []attribute.KeyValue,
	startTime time.Time,
) context.Context {
	return context.WithValue(ctx, h.key, rpcMetricContext{
		startTime:       startTime,
		startAttributes: startAttributes,
	})
}

// OnAfterStart is called after the operation starts
func (h *RpcServerMetric) OnAfterStart(context context.Context, endTime time.Time) {
	// No-op for RPC server metrics
}

// OnAfterEnd is called after the operation ends and records the metric
func (h *RpcServerMetric) OnAfterEnd(ctx context.Context, endAttributes []attribute.KeyValue, endTime time.Time) {
	mc, ok := ctx.Value(h.key).(rpcMetricContext)
	if !ok {
		return
	}

	startTime, startAttributes := mc.startTime, mc.startAttributes

	// Initialize histogram if needed
	if h.serverRequestDuration == nil {
		meter := otel.GetMeterProvider().Meter(
			"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst-api-semconv/instrumenter/rpc",
		)
		var err error
		h.serverRequestDuration, err = meter.Float64Histogram(
			rpcServerRequestDuration,
			metric.WithUnit("ms"),
			metric.WithDescription("Duration of rpc server requests."),
		)
		if err != nil {
			h.logger.Error("failed to create serverRequestDuration", "error", err)
			return
		}
	}

	// Combine start and end attributes
	allAttributes := append(endAttributes, startAttributes...)

	// Filter attributes for metrics
	n, metricsAttrs := shadowRpcMetricsAttrs(allAttributes)

	// Record the duration
	duration := float64(endTime.Sub(startTime).Milliseconds())
	h.serverRequestDuration.Record(ctx, duration, metric.WithAttributeSet(attribute.NewSet(metricsAttrs[0:n]...)))
}

// OnBeforeStart is called before the operation starts
func (h *RpcClientMetric) OnBeforeStart(parentContext context.Context, startTime time.Time) context.Context {
	return parentContext
}

// OnBeforeEnd is called before the operation ends
func (h *RpcClientMetric) OnBeforeEnd(
	ctx context.Context,
	startAttributes []attribute.KeyValue,
	startTime time.Time,
) context.Context {
	return context.WithValue(ctx, h.key, rpcMetricContext{
		startTime:       startTime,
		startAttributes: startAttributes,
	})
}

// OnAfterStart is called after the operation starts
func (h *RpcClientMetric) OnAfterStart(context context.Context, endTime time.Time) {
	// No-op for RPC client metrics
}

// OnAfterEnd is called after the operation ends and records the metric
func (h *RpcClientMetric) OnAfterEnd(ctx context.Context, endAttributes []attribute.KeyValue, endTime time.Time) {
	mc, ok := ctx.Value(h.key).(rpcMetricContext)
	if !ok {
		return
	}

	startTime, startAttributes := mc.startTime, mc.startAttributes

	// Initialize histogram if needed
	if h.clientRequestDuration == nil {
		meter := otel.GetMeterProvider().Meter(
			"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst-api-semconv/instrumenter/rpc",
		)
		var err error
		h.clientRequestDuration, err = meter.Float64Histogram(
			rpcClientRequestDuration,
			metric.WithUnit("ms"),
			metric.WithDescription("Duration of rpc client requests."),
		)
		if err != nil {
			h.logger.Error("failed to create clientRequestDuration", "error", err)
			return
		}
	}

	// Combine start and end attributes
	allAttributes := append(endAttributes, startAttributes...)

	// Filter attributes for metrics
	n, metricsAttrs := shadowRpcMetricsAttrs(allAttributes)

	// Record the duration
	duration := float64(endTime.Sub(startTime).Milliseconds())
	h.clientRequestDuration.Record(ctx, duration, metric.WithAttributeSet(attribute.NewSet(metricsAttrs[0:n]...)))
}

// shadowRpcMetricsAttrs filters attributes to only include those relevant for RPC metrics
func shadowRpcMetricsAttrs(attrs []attribute.KeyValue) (int, []attribute.KeyValue) {
	result := make([]attribute.KeyValue, 0, len(attrs))
	for _, attr := range attrs {
		if rpcMetricsConv[attr.Key] {
			result = append(result, attr)
		}
	}
	return len(result), result
}
