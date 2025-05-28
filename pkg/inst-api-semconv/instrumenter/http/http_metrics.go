// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package http

import (
	"context"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst-api-semconv/instrumenter/utils"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"
	"log"
	"sync"
	"time"
)

/**
Http Metrics is defined by https://opentelemetry.io/docs/specs/semconv/http/http-metrics/
Here are some implementations for stable metrics.
*/

const http_server_request_duration = "http.server.request.duration"

const http_client_request_duration = "http.client.request.duration"

type HttpServerMetric struct {
	key                   attribute.Key
	serverRequestDuration metric.Float64Histogram
}

type HttpClientMetric struct {
	key                   attribute.Key
	clientRequestDuration metric.Float64Histogram
}

var mu sync.Mutex

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

var globalMeter metric.Meter

func InitHttpMetrics(m metric.Meter) {
	mu.Lock()
	defer mu.Unlock()
	globalMeter = m
}

func HttpServerMetrics(key string) *HttpServerMetric {
	mu.Lock()
	defer mu.Unlock()
	return &HttpServerMetric{key: attribute.Key(key)}
}

func HttpClientMetrics(key string) *HttpClientMetric {
	mu.Lock()
	defer mu.Unlock()
	return &HttpClientMetric{key: attribute.Key(key)}
}

// for test only
func newHttpServerMetric(key string, meter metric.Meter) (*HttpServerMetric, error) {
	m := &HttpServerMetric{
		key: attribute.Key(key),
	}
	d, err := utils.NewFloat64Histogram(http_server_request_duration, "ms", "Duration of HTTP server requests.", meter)
	if err != nil {
		return nil, err
	}
	m.serverRequestDuration = d
	return m, nil
}

// for test only
func newHttpClientMetric(key string, meter metric.Meter) (*HttpClientMetric, error) {
	m := &HttpClientMetric{
		key: attribute.Key(key),
	}
	d, err := utils.NewFloat64Histogram(http_client_request_duration, "ms", "Duration of HTTP client requests.", meter)
	if err != nil {
		return nil, err
	}
	m.clientRequestDuration = d
	return m, nil
}

type httpMetricContext struct {
	startTime       time.Time
	startAttributes []attribute.KeyValue
}

func (h *HttpServerMetric) OnBeforeStart(parentContext context.Context, startTime time.Time) context.Context {
	return parentContext
}

func (h *HttpServerMetric) OnBeforeEnd(ctx context.Context, startAttributes []attribute.KeyValue, startTime time.Time) context.Context {
	return context.WithValue(ctx, h.key, httpMetricContext{
		startTime:       startTime,
		startAttributes: startAttributes,
	})
}

func (h *HttpServerMetric) OnAfterStart(context context.Context, endTime time.Time) {}

func (h *HttpServerMetric) OnAfterEnd(context context.Context, endAttributes []attribute.KeyValue, endTime time.Time) {
	mc := context.Value(h.key).(httpMetricContext)
	startTime, startAttributes := mc.startTime, mc.startAttributes
	// end attributes should be shadowed by AttrsShadower
	if h.serverRequestDuration == nil {
		var err error
		h.serverRequestDuration, err = utils.NewFloat64Histogram(http_server_request_duration, "ms",
			"Duration of HTTP server requests.", globalMeter)
		if err != nil {
			log.Printf("failed to create serverRequestDuration, err is %v\n", err)
		}
	}
	endAttributes = append(endAttributes, startAttributes...)
	n, metricsAttrs := utils.Shadow(endAttributes, httpMetricsConv)
	if h.serverRequestDuration != nil {
		h.serverRequestDuration.Record(context, float64(endTime.Sub(startTime)), metric.WithAttributeSet(attribute.NewSet(metricsAttrs[0:n]...)))
	}
}

func (h HttpClientMetric) OnBeforeStart(parentContext context.Context, startTime time.Time) context.Context {
	return parentContext
}

func (h HttpClientMetric) OnBeforeEnd(ctx context.Context, startAttributes []attribute.KeyValue, startTime time.Time) context.Context {
	return context.WithValue(ctx, h.key, httpMetricContext{
		startTime:       startTime,
		startAttributes: startAttributes,
	})
}

func (h HttpClientMetric) OnAfterStart(context context.Context, endTime time.Time) {}

func (h HttpClientMetric) OnAfterEnd(context context.Context, endAttributes []attribute.KeyValue, endTime time.Time) {
	mc := context.Value(h.key).(httpMetricContext)
	startTime, startAttributes := mc.startTime, mc.startAttributes
	// end attributes should be shadowed by AttrsShadower
	if h.clientRequestDuration == nil {
		var err error
		// second change to init the metric
		h.clientRequestDuration, err = utils.NewFloat64Histogram(http_client_request_duration, "ms",
			"Duration of HTTP client requests.", globalMeter)
		if err != nil {
			log.Printf("failed to create clientRequestDuration, err is %v\n", err)
		}
	}
	endAttributes = append(endAttributes, startAttributes...)
	n, metricsAttrs := utils.Shadow(endAttributes, httpMetricsConv)
	if h.clientRequestDuration != nil {
		h.clientRequestDuration.Record(context, float64(endTime.Sub(startTime)), metric.WithAttributeSet(attribute.NewSet(metricsAttrs[0:n]...)))
	}
}
