// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package http

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst-api-semconv/instrumenter/utils"
)

/**
HTTP Metrics is defined by https://opentelemetry.io/docs/specs/semconv/http/http-metrics/
Here are some implementations for stable metrics.
*/

const (
	httpDerverTequestDuration = "http.server.request.duration"
	httpClientRequestDuration = "http.client.request.duration"
)

// httpMetricsConv defines the attributes that should be included in HTTP metrics
// This is a read-only map, so it's safe to be a package-level variable
//
//nolint:gochecknoglobals // Read-only map, safe as package-level constant
var httpMetricsConv = map[attribute.Key]bool{
	semconv.HTTPRequestMethodKey:      true,
	semconv.URLSchemeKey:              true,
	semconv.ErrorTypeKey:              true,
	semconv.HTTPResponseStatusCodeKey: true,
	semconv.HTTPRouteKey:              true,
	semconv.NetworkProtocolNameKey:    true,
	semconv.NetworkProtocolVersionKey: true,
	semconv.ServerAddressKey:          true,
	semconv.ServerPortKey:             true,
}

// Registry is the interface for creating HTTP metrics
type Registry interface {
	// NewHTTPServerMetric creates a new HTTP server metric
	NewHTTPServerMetric(key string) (*HTTPServerMetric, error)
	// NewHTTPClientMetric creates a new HTTP client metric
	NewHTTPClientMetric(key string) (*HTTPClientMetric, error)
}

// MetricsRegistry manages HTTP metrics creation and configuration
type MetricsRegistry struct {
	logger *slog.Logger
	meter  metric.Meter
	mu     sync.RWMutex
}

// NewMetricsRegistry creates a new MetricsRegistry with the given logger and meter
func NewMetricsRegistry(logger *slog.Logger, meter metric.Meter) *MetricsRegistry {
	if logger == nil {
		logger = slog.Default()
	}
	return &MetricsRegistry{
		meter:  meter,
		logger: logger,
	}
}

// HTTPServerMetric represents HTTP server metrics
type HTTPServerMetric struct {
	key                   attribute.Key
	serverRequestDuration metric.Float64Histogram
	logger                *slog.Logger
	mu                    sync.Mutex
}

// HTTPClientMetric represents HTTP client metrics
type HTTPClientMetric struct {
	key                   attribute.Key
	clientRequestDuration metric.Float64Histogram
	logger                *slog.Logger
	mu                    sync.Mutex
}

// NewHTTPServerMetric creates a new HTTP server metric
func (r *MetricsRegistry) NewHTTPServerMetric(key string) (*HTTPServerMetric, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.meter == nil {
		return nil, errors.New("meter is not initialized")
	}

	m := &HTTPServerMetric{
		key:    attribute.Key(key),
		logger: r.logger,
	}

	// Eagerly create the histogram if meter is available
	d, err := utils.NewFloat64Histogram(httpDerverTequestDuration, "ms", "Duration of HTTP server requests.", r.meter)
	if err != nil {
		return nil, fmt.Errorf("failed to create serverRequestDuration: %w", err)
	}
	m.serverRequestDuration = d

	return m, nil
}

// NewHTTPClientMetric creates a new HTTP client metric
func (r *MetricsRegistry) NewHTTPClientMetric(key string) (*HTTPClientMetric, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.meter == nil {
		return nil, errors.New("meter is not initialized")
	}

	m := &HTTPClientMetric{
		key:    attribute.Key(key),
		logger: r.logger,
	}

	// Eagerly create the histogram if meter is available
	d, err := utils.NewFloat64Histogram(httpClientRequestDuration, "ms", "Duration of HTTP client requests.", r.meter)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientRequestDuration: %w", err)
	}
	m.clientRequestDuration = d

	return m, nil
}

// NoopRegistry is a no-op implementation of Registry for testing
type NoopRegistry struct{}

// NewNoOpRegistry creates a new no-op registry
func NewNoOpRegistry() *NoopRegistry {
	return &NoopRegistry{}
}

// NewHTTPServerMetric creates a no-op HTTP server metric
func (*NoopRegistry) NewHTTPServerMetric(key string) (*HTTPServerMetric, error) {
	return &HTTPServerMetric{
		key:    attribute.Key(key),
		logger: slog.Default(),
	}, nil
}

// NewHTTPClientMetric creates a no-op HTTP client metric
func (*NoopRegistry) NewHTTPClientMetric(key string) (*HTTPClientMetric, error) {
	return &HTTPClientMetric{
		key:    attribute.Key(key),
		logger: slog.Default(),
	}, nil
}

type httpMetricContext struct {
	startTime       time.Time
	startAttributes []attribute.KeyValue
}

func (*HTTPServerMetric) OnBeforeStart(parentContext context.Context, _ time.Time) context.Context {
	return parentContext
}

func (h *HTTPServerMetric) OnBeforeEnd(
	ctx context.Context,
	startAttributes []attribute.KeyValue,
	startTime time.Time,
) context.Context {
	return context.WithValue(ctx, h.key, httpMetricContext{
		startTime:       startTime,
		startAttributes: startAttributes,
	})
}

func (*HTTPServerMetric) OnAfterStart(_ context.Context, _ time.Time) {}

func (h *HTTPServerMetric) OnAfterEnd(context context.Context, endAttributes []attribute.KeyValue, endTime time.Time) {
	value := context.Value(h.key)
	if value == nil {
		// Context doesn't contain expected metric context, skip recording
		return
	}
	mc, ok := value.(httpMetricContext)
	if !ok {
		// Type assertion failed, skip recording
		return
	}
	startTime, startAttributes := mc.startTime, mc.startAttributes

	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if histogram is initialized
	if h.serverRequestDuration == nil {
		if h.logger != nil {
			h.logger.WarnContext(context, "serverRequestDuration is not initialized")
		}
		return
	}

	endAttributes = append(endAttributes, startAttributes...)
	n, metricsAttrs := utils.Shadow(endAttributes, httpMetricsConv)
	h.serverRequestDuration.Record(
		context,
		float64(endTime.Sub(startTime)),
		metric.WithAttributeSet(attribute.NewSet(metricsAttrs[0:n]...)),
	)
}

func (*HTTPClientMetric) OnBeforeStart(parentContext context.Context, _ time.Time) context.Context {
	return parentContext
}

func (h *HTTPClientMetric) OnBeforeEnd(
	ctx context.Context,
	startAttributes []attribute.KeyValue,
	startTime time.Time,
) context.Context {
	return context.WithValue(ctx, h.key, httpMetricContext{
		startTime:       startTime,
		startAttributes: startAttributes,
	})
}

func (*HTTPClientMetric) OnAfterStart(_ context.Context, _ time.Time) {}

func (h *HTTPClientMetric) OnAfterEnd(context context.Context, endAttributes []attribute.KeyValue, endTime time.Time) {
	value := context.Value(h.key)
	if value == nil {
		// Context doesn't contain expected metric context, skip recording
		return
	}
	mc, ok := value.(httpMetricContext)
	if !ok {
		// Type assertion failed, skip recording
		return
	}
	startTime, startAttributes := mc.startTime, mc.startAttributes

	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if histogram is initialized
	if h.clientRequestDuration == nil {
		if h.logger != nil {
			h.logger.WarnContext(context, "clientRequestDuration is not initialized")
		}
		return
	}

	endAttributes = append(endAttributes, startAttributes...)
	n, metricsAttrs := utils.Shadow(endAttributes, httpMetricsConv)
	h.clientRequestDuration.Record(
		context,
		float64(endTime.Sub(startTime)),
		metric.WithAttributeSet(attribute.NewSet(metricsAttrs[0:n]...)),
	)
}
