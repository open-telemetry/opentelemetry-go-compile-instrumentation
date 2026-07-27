// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestSpanName(t *testing.T) {
	tests := []struct {
		method, endpoint, want string
	}{
		{"GET", "linode/instances/1", "GET linode/instances/1"},
		{"get", "linode/instances", "GET linode/instances"},
		{"POST", "", "POST"},
		{"", "regions", "HTTP regions"},
		{"", "", "HTTP"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, SpanName(tt.method, tt.endpoint))
		})
	}
}

func TestOperationSpanName(t *testing.T) {
	assert.Equal(t, "linodego.GetInstance", OperationSpanName("GetInstance"))
	assert.Equal(t, "linodego.operation", OperationSpanName(""))
}

func TestLinodegoRequestTraceAttrs(t *testing.T) {
	attrs := LinodegoRequestTraceAttrs(LinodegoRequest{
		Method:   "GET",
		Endpoint: "linode/instances/1",
	})
	m := attrsToMap(attrs)
	assert.Equal(t, "GET", m["http.request.method"])
	assert.Equal(t, "/linode/instances/1", m["url.path"])
	assert.Equal(t, DefaultServerAddress, m["server.address"])

	attrs = LinodegoRequestTraceAttrs(LinodegoRequest{
		Method:        "POST",
		Endpoint:      "/v4/linode/instances",
		ServerAddress: "api.test.linode.com",
	})
	m = attrsToMap(attrs)
	assert.Equal(t, "POST", m["http.request.method"])
	assert.Equal(t, "/v4/linode/instances", m["url.path"])
	assert.Equal(t, "api.test.linode.com", m["server.address"])
	_, hasFunc := m["code.function.name"]
	assert.False(t, hasFunc, "request spans do not set code.function.name")
}

func TestLinodegoOperationTraceAttrs(t *testing.T) {
	m := attrsToMap(LinodegoOperationTraceAttrs("ListVolumes"))
	assert.Equal(t, DefaultServerAddress, m["server.address"])
	assert.Equal(t, "ListVolumes", m["code.function.name"])
}

type statusErr struct {
	code int
	msg  string
}

func (e statusErr) Error() string   { return e.msg }
func (e statusErr) StatusCode() int { return e.code }

func TestStatusCodeFromError(t *testing.T) {
	code, ok := StatusCodeFromError(nil)
	assert.False(t, ok)
	assert.Equal(t, 0, code)

	_, ok = StatusCodeFromError(errors.New("plain"))
	assert.False(t, ok)

	code, ok = StatusCodeFromError(statusErr{code: 404, msg: "not found"})
	require.True(t, ok)
	assert.Equal(t, 404, code)

	// Wrapped errors must still yield the status code.
	wrapped := fmt.Errorf("linode request failed: %w", statusErr{code: 404, msg: "not found"})
	code, ok = StatusCodeFromError(wrapped)
	require.True(t, ok)
	assert.Equal(t, 404, code)

	code, ok = StatusCodeFromError(statusErr{code: 2, msg: "from stringer"})
	assert.False(t, ok)
	assert.Equal(t, 0, code)
}

func TestLinodegoErrorTraceAttrs(t *testing.T) {
	assert.Nil(t, LinodegoErrorTraceAttrs(nil))

	attrs := LinodegoErrorTraceAttrs(statusErr{code: 404, msg: "not found"})
	m := attrsToMap(attrs)
	assert.Equal(t, int64(404), m["http.response.status_code"])
	assert.Equal(t, "404", m["error.type"])

	attrs = LinodegoErrorTraceAttrs(statusErr{code: 200, msg: "ok but err?"})
	m = attrsToMap(attrs)
	assert.Equal(t, int64(200), m["http.response.status_code"])
	_, hasErrType := m["error.type"]
	assert.False(t, hasErrType)
}

func TestHTTPClientStatus(t *testing.T) {
	sc, _ := HTTPClientStatus(200)
	assert.Equal(t, codes.Unset, sc)

	sc, _ = HTTPClientStatus(404)
	assert.Equal(t, codes.Error, sc)

	sc, desc := HTTPClientStatus(0)
	assert.Equal(t, codes.Error, sc)
	assert.Contains(t, desc, "Invalid")
}

func TestMetricsRecord(t *testing.T) {
	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	m := NewMetrics(mp.Meter("test"))
	ctx := context.Background()
	m.RecordOperationDuration(ctx, 0.12, "GetInstance", 200)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))
	require.NotEmpty(t, rm.ScopeMetrics)

	names := map[string]bool{}
	for _, sm := range rm.ScopeMetrics {
		for _, met := range sm.Metrics {
			names[met.Name] = true
		}
	}
	assert.True(t, names[MetricOperationDuration], "expected operation duration metric")
	assert.False(t, names["linodego.client.request.duration"], "request duration should not be emitted")
}

func TestMetricAttributes_ModerateCardinality(t *testing.T) {
	m := attrsToMap(MetricAttributes("GetInstance", 404))
	assert.Equal(t, DefaultServerAddress, m["server.address"])
	assert.Equal(t, "GetInstance", m["code.function.name"])
	assert.Equal(t, int64(404), m["http.response.status_code"])

	// Unbounded path-style labels must not appear on metrics.
	_, hasPath := m["url.path"]
	_, hasMethod := m["http.request.method"]
	assert.False(t, hasPath)
	assert.False(t, hasMethod)

	// Success path without a status code omits the status attribute.
	m = attrsToMap(MetricAttributes("ListRegions", 0))
	assert.Equal(t, "ListRegions", m["code.function.name"])
	_, hasStatus := m["http.response.status_code"]
	assert.False(t, hasStatus)
}

func TestNewMetricsNilMeter(t *testing.T) {
	m := NewMetrics(nil)
	// Should not panic.
	m.RecordOperationDuration(context.Background(), 1, "GetInstance", 0)
}

func attrsToMap(attrs []attribute.KeyValue) map[string]interface{} {
	m := make(map[string]interface{}, len(attrs))
	for _, a := range attrs {
		switch a.Value.Type() {
		case attribute.STRING:
			m[string(a.Key)] = a.Value.AsString()
		case attribute.INT64:
			m[string(a.Key)] = a.Value.AsInt64()
		default:
			m[string(a.Key)] = fmt.Sprint(a.Value.AsInterface())
		}
	}
	return m
}
