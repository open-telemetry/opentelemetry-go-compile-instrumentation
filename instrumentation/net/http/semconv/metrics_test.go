// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

type int64Add struct {
	attributes attribute.Set
	value      int64
}

type int64AddRecorder struct {
	noop.Int64UpDownCounter
	adds *[]int64Add
}

func (r int64AddRecorder) Add(_ context.Context, value int64, opts ...metric.AddOption) {
	*r.adds = append(*r.adds, int64Add{
		attributes: metric.NewAddConfig(opts).Attributes(),
		value:      value,
	})
}

type activeRequestMeter struct {
	noop.Meter
	adds       *[]int64Add
	metricName string
}

type histogramConfigMeter struct {
	noop.Meter
	configs map[string]metric.Float64HistogramConfig
}

func (m histogramConfigMeter) Float64Histogram(
	name string,
	opts ...metric.Float64HistogramOption,
) (metric.Float64Histogram, error) {
	m.configs[name] = metric.NewFloat64HistogramConfig(opts...)
	return noop.Float64Histogram{}, nil
}

func (m activeRequestMeter) Int64UpDownCounter(
	name string,
	opts ...metric.Int64UpDownCounterOption,
) (metric.Int64UpDownCounter, error) {
	if name == m.metricName {
		return int64AddRecorder{adds: m.adds}, nil
	}
	return m.Meter.Int64UpDownCounter(name, opts...)
}

func TestHTTPClientActiveRequests(t *testing.T) {
	req := &http.Request{
		Method: http.MethodPost,
		URL: &url.URL{
			Scheme: "https",
			Host:   "example.com",
		},
		Header: http.Header{},
		Proto:  "HTTP/2.0",
	}
	customAttr := attribute.String("test.attribute", "value")
	client := NewHTTPClient(nil)

	attrs := client.ActiveRequestMetricAttributes(req, []attribute.KeyValue{customAttr})

	assert.ElementsMatch(t, []attribute.KeyValue{
		customAttr,
		semconv.HTTPRequestMethodPost,
		semconv.ServerAddress("example.com"),
		semconv.URLScheme("https"),
	}, attrs)

	var adds []int64Add
	client = NewHTTPClient(activeRequestMeter{
		adds:       &adds,
		metricName: "http.client.active_requests",
	})
	set := attribute.NewSet(attrs...)

	client.AddActiveRequests(context.Background(), 1, set)

	require.Len(t, adds, 1)
	assert.Equal(t, int64(1), adds[0].value)
	assert.Equal(t, set.Equivalent(), adds[0].attributes.Equivalent())
}

func TestHTTPServerActiveRequests(t *testing.T) {
	req := &http.Request{
		Method: http.MethodGet,
		Host:   "example.com",
		URL:    &url.URL{},
		Header: http.Header{},
		Proto:  "HTTP/1.1",
	}
	customAttr := attribute.String("test.attribute", "value")
	server := NewHTTPServer(nil)

	attrs := server.ActiveRequestMetricAttributes("", req, []attribute.KeyValue{customAttr})

	assert.ElementsMatch(t, []attribute.KeyValue{
		customAttr,
		semconv.HTTPRequestMethodGet,
		semconv.URLScheme("http"),
		semconv.ServerAddress("example.com"),
	}, attrs)

	var adds []int64Add
	server = NewHTTPServer(activeRequestMeter{
		adds:       &adds,
		metricName: "http.server.active_requests",
	})
	set := attribute.NewSet(attrs...)

	server.AddActiveRequests(context.Background(), -1, set)

	require.Len(t, adds, 1)
	assert.Equal(t, int64(-1), adds[0].value)
	assert.Equal(t, set.Equivalent(), adds[0].attributes.Equivalent())
}

func TestHTTPRequestDurationBucketBoundaries(t *testing.T) {
	configs := make(map[string]metric.Float64HistogramConfig)
	meter := histogramConfigMeter{configs: configs}

	NewHTTPClient(meter)
	NewHTTPServer(meter)

	expected := []float64{
		0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10,
	}
	assert.Equal(
		t,
		expected,
		configs["http.client.request.duration"].ExplicitBucketBoundaries(),
	)
	assert.Equal(
		t,
		expected,
		configs["http.server.request.duration"].ExplicitBucketBoundaries(),
	)
}
