// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package db

import (
	"context"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst-api-semconv/instrumenter/utils"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"
	"testing"
	"time"
)

func TestDbClientMetrics(t *testing.T) {
	reader := metric.NewManualReader()
	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName("my-service"),
		semconv.ServiceVersion("v0.1.0"),
	)
	mp := metric.NewMeterProvider(metric.WithResource(res), metric.WithReader(reader))
	meter := mp.Meter("test-meter")
	client, err := newDbClientMetric("test", meter)
	if err != nil {
		panic(err)
	}
	ctx := context.Background()
	start := time.Now()
	ctx = client.OnBeforeStart(ctx, start)
	ctx = client.OnBeforeEnd(ctx, []attribute.KeyValue{}, start)
	client.OnAfterStart(ctx, start)
	client.OnAfterEnd(ctx, []attribute.KeyValue{}, time.Now())
	rm := &metricdata.ResourceMetrics{}
	reader.Collect(ctx, rm)
	if rm.ScopeMetrics[0].Metrics[0].Name != "db.client.request.duration" {
		panic("wrong metrics name, " + rm.ScopeMetrics[0].Metrics[0].Name)
	}
}

func TestDbMetricAttributesShadower(t *testing.T) {
	attrs := make([]attribute.KeyValue, 0)
	attrs = append(attrs, attribute.KeyValue{
		Key:   semconv.DBSystemNameKey,
		Value: attribute.StringValue("mysql"),
	}, attribute.KeyValue{
		Key:   "unknown",
		Value: attribute.Value{},
	}, attribute.KeyValue{
		Key:   semconv.DBOperationNameKey,
		Value: attribute.StringValue("Db"),
	}, attribute.KeyValue{
		Key:   semconv.ServerAddressKey,
		Value: attribute.StringValue("abc"),
	})
	n, attrs := utils.Shadow(attrs, dbMetricsConv)
	if n != 3 {
		panic("wrong shadow array")
	}
	if attrs[3].Key != "unknown" {
		panic("unknown should be the last attribute")
	}
}

func TestLazyDbClientMetrics(t *testing.T) {
	reader := metric.NewManualReader()
	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName("my-service"),
		semconv.ServiceVersion("v0.1.0"),
	)
	mp := metric.NewMeterProvider(metric.WithResource(res), metric.WithReader(reader))
	m := mp.Meter("test-meter")
	InitDbMetrics(m)
	client := DbClientMetrics("db.client")
	ctx := context.Background()
	start := time.Now()
	ctx = client.OnBeforeStart(ctx, start)
	ctx = client.OnBeforeEnd(ctx, []attribute.KeyValue{}, start)
	client.OnAfterStart(ctx, start)
	client.OnAfterEnd(ctx, []attribute.KeyValue{}, time.Now())
	rm := &metricdata.ResourceMetrics{}
	reader.Collect(ctx, rm)
	if rm.ScopeMetrics[0].Metrics[0].Name != "db.client.request.duration" {
		panic("wrong metrics name, " + rm.ScopeMetrics[0].Metrics[0].Name)
	}
}

func TestGlobalDbClientMetrics(t *testing.T) {
	reader := metric.NewManualReader()
	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName("my-service"),
		semconv.ServiceVersion("v0.1.0"),
	)
	mp := metric.NewMeterProvider(metric.WithResource(res), metric.WithReader(reader))
	m := mp.Meter("test-meter")
	InitDbMetrics(m)
	client := DbClientMetrics("db.client")
	ctx := context.Background()
	start := time.Now()
	ctx = client.OnBeforeStart(ctx, start)
	ctx = client.OnBeforeEnd(ctx, []attribute.KeyValue{}, start)
	client.OnAfterStart(ctx, start)
	client.OnAfterEnd(ctx, []attribute.KeyValue{}, time.Now())
	rm := &metricdata.ResourceMetrics{}
	reader.Collect(ctx, rm)
	if rm.ScopeMetrics[0].Metrics[0].Name != "db.client.request.duration" {
		panic("wrong metrics name, " + rm.ScopeMetrics[0].Metrics[0].Name)
	}
}

func TestNilMeter(t *testing.T) {
	reader := metric.NewManualReader()
	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName("my-service"),
		semconv.ServiceVersion("v0.1.0"),
	)
	_ = metric.NewMeterProvider(metric.WithResource(res), metric.WithReader(reader))
	_, err := newDbClientMetric("test", nil)
	if err == nil {
		panic(err)
	}
}
