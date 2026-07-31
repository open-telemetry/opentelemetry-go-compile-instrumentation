// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package v2

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/linode/linodego/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otelc/pkg/hook/hooktest"
)

func setupTestProviders(t *testing.T) (*tracetest.SpanRecorder, *sdkmetric.ManualReader) {
	t.Helper()
	// Reset package-level init so each test binds to the test providers.
	initOnce = sync.Once{}
	tracer = nil

	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)

	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		_ = mp.Shutdown(context.Background())
	})
	return sr, reader
}

func TestLinodegoEnabler(t *testing.T) {
	tests := []struct {
		name     string
		setupEnv func(t *testing.T)
		expected bool
	}{
		{
			name: "enabled explicitly",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "linodego")
			},
			expected: true,
		},
		{
			name: "disabled explicitly",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "linodego")
			},
			expected: false,
		},
		{
			name: "not in enabled list",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp")
			},
			expected: false,
		},
		{
			name:     "default enabled when no env set",
			setupEnv: func(t *testing.T) {},
			expected: true,
		},
		{
			name: "enabled with multiple instrumentations",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp,linodego,grpc")
			},
			expected: true,
		},
		{
			name: "disabled with multiple instrumentations",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "linodego,grpc")
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupEnv(t)
			assert.Equal(t, tt.expected, enabler.Enable())
		})
	}
}

func TestInstrumentationConstants(t *testing.T) {
	assert.Equal(t,
		"go.opentelemetry.io/otelc/instrumentation/github.com/linode/linodego/v2",
		instrumentationName,
	)
	assert.Equal(t, "LINODEGO", instrumentationKey)
}

func TestBeforeAfterDoRequest_Success(t *testing.T) {
	sr, reader := setupTestProviders(t)
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "linodego")

	parent := context.Background()
	ictx := hooktest.NewMockHookContext(
		(*linodego.Client)(nil),
		parent,
		"GET",
		"linode/instances/1",
		nil,
		nil,
	)

	BeforeDoRequest(ictx, nil, parent, "GET", "linode/instances/1", nil, nil)

	newCtx, ok := ictx.GetParam(ctxParamIndex).(context.Context)
	require.True(t, ok)
	require.True(t, trace.SpanContextFromContext(newCtx).IsValid())

	span, ok := ictx.GetKeyData(keySpan).(trace.Span)
	require.True(t, ok)
	require.NotNil(t, span)

	AfterDoRequest(ictx, nil)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	got := spans[0]
	assert.Equal(t, "GET linode/instances/1", got.Name())
	assert.Equal(t, trace.SpanKindClient, got.SpanKind())
	assert.Equal(t, codes.Unset, got.Status().Code)

	attrs := attrMap(got.Attributes())
	assert.Equal(t, "GET", attrs["http.request.method"])
	assert.Equal(t, "/linode/instances/1", attrs["url.path"])
	assert.Equal(t, "api.linode.com", attrs["server.address"])

	// doRequest does not emit metrics (operation-level only).
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	require.False(t, hasMetric(rm, "linodego.client.request.duration"))
	require.False(t, hasMetric(rm, "linodego.client.operation.duration"))
}

func TestBeforeAfterDoRequest_ErrorWithStatus(t *testing.T) {
	sr, _ := setupTestProviders(t)
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "linodego")

	parent := context.Background()
	ictx := hooktest.NewMockHookContext()

	BeforeDoRequest(ictx, nil, parent, "GET", "linode/instances/999", nil, nil)
	AfterDoRequest(ictx, linodego.Error{Code: 404, Message: "Not Found"})

	spans := sr.Ended()
	require.Len(t, spans, 1)
	got := spans[0]
	assert.Equal(t, codes.Error, got.Status().Code)
	attrs := attrMap(got.Attributes())
	assert.Equal(t, int64(404), attrs["http.response.status_code"])
	assert.Equal(t, "404", attrs["error.type"])
	require.NotEmpty(t, got.Events(), "expected RecordError event")
}

func TestBeforeAfterDoRequest_PlainError(t *testing.T) {
	sr, _ := setupTestProviders(t)
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "linodego")

	parent := context.Background()
	ictx := hooktest.NewMockHookContext()

	BeforeDoRequest(ictx, nil, parent, "GET", "linode/instances/1", nil, nil)
	AfterDoRequest(ictx, errors.New("network down"))

	spans := sr.Ended()
	require.Len(t, spans, 1)
	got := spans[0]
	assert.Equal(t, codes.Error, got.Status().Code)
	assert.Equal(t, "network down", got.Status().Description)
}

func TestBeforeDoRequest_Disabled(t *testing.T) {
	sr, _ := setupTestProviders(t)
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "linodego")

	parent := context.Background()
	ictx := hooktest.NewMockHookContext(nil, parent, "GET", "x", nil, nil)

	BeforeDoRequest(ictx, nil, parent, "GET", "x", nil, nil)
	AfterDoRequest(ictx, nil)

	assert.Empty(t, sr.Ended())
	assert.Nil(t, ictx.GetKeyData(keySpan))
}

func TestPublicMethodHooks_Success(t *testing.T) {
	sr, reader := setupTestProviders(t)
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "linodego")

	parent := context.Background()
	ictx := hooktest.NewMockHookContext(nil, parent, 123)
	ictx.FuncName = "GetInstance"

	BeforeAPICall2(ictx, nil, parent, 123)

	newCtx, ok := ictx.GetParam(1).(context.Context)
	require.True(t, ok)
	require.True(t, trace.SpanContextFromContext(newCtx).IsValid())

	AfterAPICall2(ictx, nil, nil)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	got := spans[0]
	assert.Equal(t, "linodego.GetInstance", got.Name())
	assert.Equal(t, trace.SpanKindClient, got.SpanKind())
	attrs := attrMap(got.Attributes())
	assert.Equal(t, "GetInstance", attrs["code.function.name"])

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	require.True(t, hasMetric(rm, "linodego.client.operation.duration"))
	require.False(t, hasMetric(rm, "linodego.client.request.duration"))
}

func TestPublicMethodHooks_Error(t *testing.T) {
	sr, _ := setupTestProviders(t)
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "linodego")

	parent := context.Background()
	ictx := hooktest.NewMockHookContext(nil, parent, 999)
	ictx.FuncName = "GetInstance"

	BeforeAPICall2(ictx, nil, parent, 999)
	AfterAPICall2(ictx, nil, linodego.Error{Code: 404, Message: "Not Found"})

	spans := sr.Ended()
	require.Len(t, spans, 1)
	got := spans[0]
	assert.Equal(t, codes.Error, got.Status().Code)
	attrs := attrMap(got.Attributes())
	assert.Equal(t, int64(404), attrs["http.response.status_code"])
}

func TestPublicMethodHooks_Disabled(t *testing.T) {
	sr, _ := setupTestProviders(t)
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "linodego")

	ictx := hooktest.NewMockHookContext(nil, context.Background())
	ictx.FuncName = "ListRegions"
	BeforeAPICall1(ictx, nil, context.Background())
	AfterAPICall2(ictx, nil, nil)
	assert.Empty(t, sr.Ended())
}

func attrMap(attrs []attribute.KeyValue) map[string]interface{} {
	m := make(map[string]interface{}, len(attrs))
	for _, a := range attrs {
		switch a.Value.Type() {
		case attribute.STRING:
			m[string(a.Key)] = a.Value.AsString()
		case attribute.INT64:
			m[string(a.Key)] = a.Value.AsInt64()
		default:
			m[string(a.Key)] = a.Value.AsInterface()
		}
	}
	return m
}

func hasMetric(rm metricdata.ResourceMetrics, name string) bool {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				return true
			}
		}
	}
	return false
}
