// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otelc/pkg/hook"
	"go.opentelemetry.io/otelc/pkg/hook/hooktest"
)

func setupTestTracer(t *testing.T) (*tracetest.SpanRecorder, *sdktrace.TracerProvider) {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return sr, tp
}

func setupTestMeter(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(provider)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	return reader
}

func TestBeforeRoundTrip(t *testing.T) {
	tests := []struct {
		name            string
		setupEnv        func(t *testing.T)
		setupRequest    func() *http.Request
		expectSpan      bool
		validateSpan    func(*testing.T, trace.Span)
		validateRequest func(*testing.T, *http.Request)
	}{
		{
			name: "basic request creates span",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp")
			},
			setupRequest: func() *http.Request {
				req, _ := http.NewRequest("GET", "http://example.com/path", nil)
				return req
			},
			expectSpan: true,
			validateSpan: func(t *testing.T, span trace.Span) {
				assert.NotNil(t, span)
			},
			validateRequest: func(t *testing.T, req *http.Request) {
				// Should have trace headers injected
				assert.NotEmpty(t, req.Header.Get("traceparent"))
			},
		},
		{
			name: "instrumentation disabled",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "nethttp")
			},
			setupRequest: func() *http.Request {
				req, _ := http.NewRequest("GET", "http://example.com/path", nil)
				return req
			},
			expectSpan: false,
		},
		{
			name: "OTel exporter request filtered",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp")
			},
			setupRequest: func() *http.Request {
				req, _ := http.NewRequest("POST", "http://localhost:4318/v1/traces", nil)
				req.Header.Set("User-Agent", "OTel OTLP Exporter Go/1.0")
				return req
			},
			expectSpan: false,
		},
		{
			name: "POST request",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp")
			},
			setupRequest: func() *http.Request {
				req, _ := http.NewRequest("POST", "http://example.com/api/data", nil)
				return req
			},
			expectSpan: true,
		},
		{
			name: "request with existing context",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp")
			},
			setupRequest: func() *http.Request {
				ctx := context.WithValue(context.Background(), "test-key", "test-value")
				req, _ := http.NewRequestWithContext(ctx, "GET", "http://example.com/path", nil)
				return req
			},
			expectSpan: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset initialization for each test by creating a new once
			initOnce = *new(sync.Once)

			tt.setupEnv(t)
			sr, _ := setupTestTracer(t)

			req := tt.setupRequest()
			mockCtx := hooktest.NewMockHookContext()
			transport := &http.Transport{}

			BeforeRoundTrip(mockCtx, transport, req)

			if tt.expectSpan {
				spans := sr.Ended()
				// Span should not be ended yet in Before hook
				assert.Equal(t, 0, len(spans), "span should not be ended in Before hook")

				// Check that data was stored
				data, ok := mockCtx.GetData().(map[string]interface{})
				require.True(t, ok, "data should be stored")
				require.NotNil(t, data, "data should not be nil")

				span, ok := data["span"].(trace.Span)
				require.True(t, ok, "span should be in data")
				require.NotNil(t, span, "span should not be nil")

				if tt.validateSpan != nil {
					tt.validateSpan(t, span)
				}

				// Check that request was updated with new context
				newReq, ok := mockCtx.GetParam(1).(*http.Request)
				require.True(t, ok, "param 1 should be request")
				require.NotNil(t, newReq, "updated request should not be nil")

				if tt.validateRequest != nil {
					tt.validateRequest(t, newReq)
				}
			} else {
				// No span should be created
				data := mockCtx.GetData()
				assert.Nil(t, data, "no data should be stored when instrumentation disabled")
			}
		})
	}
}

func TestAfterRoundTrip(t *testing.T) {
	tests := []struct {
		name         string
		setupEnv     func(t *testing.T)
		setupContext func(*sdktrace.TracerProvider) hook.HookContext
		response     *http.Response
		err          error
		validateSpan func(*testing.T, []sdktrace.ReadOnlySpan)
	}{
		{
			name: "successful response",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp")
			},
			setupContext: func(tp *sdktrace.TracerProvider) hook.HookContext {
				testTracer := tp.Tracer(instrumentationName)
				req, _ := http.NewRequest("GET", "http://example.com/path", nil)
				ctx, span := testTracer.Start(context.Background(), "GET", trace.WithSpanKind(trace.SpanKindClient))

				mockCtx := hooktest.NewMockHookContext()
				mockCtx.SetData(map[string]interface{}{
					"ctx":  ctx,
					"span": span,
					"req":  req,
				})
				return mockCtx
			},
			response: &http.Response{
				StatusCode: 200,
				Request:    httptest.NewRequest("GET", "http://example.com/path", nil),
			},
			err: nil,
			validateSpan: func(t *testing.T, spans []sdktrace.ReadOnlySpan) {
				require.Len(t, spans, 1)
				span := spans[0]
				assert.Equal(t, codes.Unset, span.Status().Code)
			},
		},
		{
			name: "error response",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp")
			},
			setupContext: func(tp *sdktrace.TracerProvider) hook.HookContext {
				testTracer := tp.Tracer(instrumentationName)
				req, _ := http.NewRequest("GET", "http://example.com/path", nil)
				ctx, span := testTracer.Start(context.Background(), "GET", trace.WithSpanKind(trace.SpanKindClient))

				mockCtx := hooktest.NewMockHookContext()
				mockCtx.SetData(map[string]interface{}{
					"ctx":  ctx,
					"span": span,
					"req":  req,
				})
				return mockCtx
			},
			response: nil,
			err:      errors.New("connection refused"),
			validateSpan: func(t *testing.T, spans []sdktrace.ReadOnlySpan) {
				require.Len(t, spans, 1)
				span := spans[0]
				assert.Equal(t, codes.Error, span.Status().Code)
				assert.Contains(t, span.Status().Description, "connection refused")

				// Check that error was recorded
				events := span.Events()
				require.Len(t, events, 1)
				assert.Equal(t, "exception", events[0].Name)
			},
		},
		{
			name: "4xx client error",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp")
			},
			setupContext: func(tp *sdktrace.TracerProvider) hook.HookContext {
				testTracer := tp.Tracer(instrumentationName)
				req, _ := http.NewRequest("GET", "http://example.com/path", nil)
				ctx, span := testTracer.Start(context.Background(), "GET", trace.WithSpanKind(trace.SpanKindClient))

				mockCtx := hooktest.NewMockHookContext()
				mockCtx.SetData(map[string]interface{}{
					"ctx":  ctx,
					"span": span,
					"req":  req,
				})
				return mockCtx
			},
			response: &http.Response{
				StatusCode: 404,
				Request:    httptest.NewRequest("GET", "http://example.com/path", nil),
			},
			err: nil,
			validateSpan: func(t *testing.T, spans []sdktrace.ReadOnlySpan) {
				require.Len(t, spans, 1)
				span := spans[0]
				// 4xx is an error for HTTP client requests per OTel HTTP semconv
				assert.Equal(t, codes.Error, span.Status().Code)
			},
		},
		{
			name: "5xx server error",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp")
			},
			setupContext: func(tp *sdktrace.TracerProvider) hook.HookContext {
				testTracer := tp.Tracer(instrumentationName)
				req, _ := http.NewRequest("GET", "http://example.com/path", nil)
				ctx, span := testTracer.Start(context.Background(), "GET", trace.WithSpanKind(trace.SpanKindClient))

				mockCtx := hooktest.NewMockHookContext()
				mockCtx.SetData(map[string]interface{}{
					"ctx":  ctx,
					"span": span,
					"req":  req,
				})
				return mockCtx
			},
			response: &http.Response{
				StatusCode: 500,
				Request:    httptest.NewRequest("GET", "http://example.com/path", nil),
			},
			err: nil,
			validateSpan: func(t *testing.T, spans []sdktrace.ReadOnlySpan) {
				require.Len(t, spans, 1)
				span := spans[0]
				assert.Equal(t, codes.Error, span.Status().Code)
			},
		},
		{
			name: "no data in context",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp")
			},
			setupContext: func(tp *sdktrace.TracerProvider) hook.HookContext {
				return hooktest.NewMockHookContext()
			},
			response: &http.Response{
				StatusCode: 200,
				Request:    httptest.NewRequest("GET", "http://example.com/path", nil),
			},
			err: nil,
			validateSpan: func(t *testing.T, spans []sdktrace.ReadOnlySpan) {
				// No span should be ended
				assert.Equal(t, 0, len(spans))
			},
		},
		{
			name: "instrumentation disabled",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "nethttp")
			},
			setupContext: func(tp *sdktrace.TracerProvider) hook.HookContext {
				testTracer := tp.Tracer(instrumentationName)
				req, _ := http.NewRequest("GET", "http://example.com/path", nil)
				ctx, span := testTracer.Start(context.Background(), "GET", trace.WithSpanKind(trace.SpanKindClient))

				mockCtx := hooktest.NewMockHookContext()
				mockCtx.SetData(map[string]interface{}{
					"ctx":  ctx,
					"span": span,
					"req":  req,
				})
				return mockCtx
			},
			response: &http.Response{
				StatusCode: 200,
				Request:    httptest.NewRequest("GET", "http://example.com/path", nil),
			},
			err: nil,
			validateSpan: func(t *testing.T, spans []sdktrace.ReadOnlySpan) {
				// Span should not be ended because instrumentation is disabled
				assert.Equal(t, 0, len(spans))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset initialization for each test by creating a new once
			initOnce = *new(sync.Once)

			tt.setupEnv(t)
			sr, tp := setupTestTracer(t)

			mockCtx := tt.setupContext(tp)

			AfterRoundTrip(mockCtx, tt.response, tt.err)

			spans := sr.Ended()
			if tt.validateSpan != nil {
				tt.validateSpan(t, spans)
			}
		})
	}
}

func TestClientEnabler(t *testing.T) {
	tests := []struct {
		name     string
		setupEnv func(t *testing.T)
		expected bool
	}{
		{
			name: "enabled explicitly",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp")
			},
			expected: true,
		},
		{
			name: "disabled explicitly",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "nethttp")
			},
			expected: false,
		},
		{
			name: "not in enabled list",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "grpc")
			},
			expected: false,
		},
		{
			name: "default enabled when no env set",
			setupEnv: func(t *testing.T) {
				// No environment variables set - should be enabled by default
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupEnv(t)

			enabler := netHttpClientEnabler{}
			result := enabler.Enable()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRoundTripRecordsRequestMetrics(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp")
	initOnce = *new(sync.Once)
	setupTestTracer(t)
	reader := setupTestMeter(t)

	req := httptest.NewRequest(http.MethodPost, "https://example.com/items", nil)
	req.ContentLength = 12
	originalProto := req.Proto
	ctx := hooktest.NewMockHookContext()

	BeforeRoundTrip(ctx, http.DefaultTransport.(*http.Transport), req)
	AfterRoundTrip(ctx, &http.Response{
		StatusCode:    http.StatusBadRequest,
		ContentLength: 34,
		Proto:         "HTTP/2.0",
		Request:       req,
	}, nil)
	assert.Equal(t, originalProto, req.Proto)

	var data metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &data))

	var names []string
	var activeRequests int64
	var durationAttrs attribute.Set
	var requestSize int64
	var responseSize int64
	for _, scope := range data.ScopeMetrics {
		for _, m := range scope.Metrics {
			names = append(names, m.Name)
			switch m.Name {
			case "http.client.active_requests":
				sum, ok := m.Data.(metricdata.Sum[int64])
				require.True(t, ok)
				require.Len(t, sum.DataPoints, 1)
				activeRequests = sum.DataPoints[0].Value
			case "http.client.request.body.size":
				histogram, ok := m.Data.(metricdata.Histogram[int64])
				require.True(t, ok)
				require.Len(t, histogram.DataPoints, 1)
				requestSize = histogram.DataPoints[0].Sum
			case "http.client.response.body.size":
				histogram, ok := m.Data.(metricdata.Histogram[int64])
				require.True(t, ok)
				require.Len(t, histogram.DataPoints, 1)
				responseSize = histogram.DataPoints[0].Sum
			case "http.client.request.duration":
				histogram, ok := m.Data.(metricdata.Histogram[float64])
				require.True(t, ok)
				require.Len(t, histogram.DataPoints, 1)
				durationAttrs = histogram.DataPoints[0].Attributes
			}
		}
	}
	assert.ElementsMatch(t, []string{
		"http.client.active_requests",
		"http.client.request.body.size",
		"http.client.response.body.size",
		"http.client.request.duration",
	}, names)
	assert.Zero(t, activeRequests)
	assert.Equal(t, int64(12), requestSize)
	assert.Equal(t, int64(34), responseSize)
	protocolVersion, ok := durationAttrs.Value(semconv.NetworkProtocolVersionKey)
	require.True(t, ok)
	assert.Equal(t, "2.0", protocolVersion.AsString())
	errorType, ok := durationAttrs.Value(semconv.ErrorTypeKey)
	require.True(t, ok)
	assert.Equal(t, "400", errorType.AsString())
}

func TestRoundTripRecordsErrorMetrics(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp")
	initOnce = *new(sync.Once)
	setupTestTracer(t)
	reader := setupTestMeter(t)

	req := httptest.NewRequest(http.MethodGet, "https://example.com/items", nil)
	ctx := hooktest.NewMockHookContext()

	BeforeRoundTrip(ctx, http.DefaultTransport.(*http.Transport), req)
	AfterRoundTrip(ctx, nil, errors.New("connection refused"))

	var data metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &data))

	var names []string
	var durationAttrs attribute.Set
	for _, scope := range data.ScopeMetrics {
		for _, m := range scope.Metrics {
			names = append(names, m.Name)
			if m.Name == "http.client.request.duration" {
				histogram, ok := m.Data.(metricdata.Histogram[float64])
				require.True(t, ok)
				require.Len(t, histogram.DataPoints, 1)
				durationAttrs = histogram.DataPoints[0].Attributes
			}
		}
	}
	assert.ElementsMatch(t, []string{
		"http.client.active_requests",
		"http.client.request.duration",
	}, names)
	errorType, ok := durationAttrs.Value(semconv.ErrorTypeKey)
	require.True(t, ok)
	assert.NotEmpty(t, errorType.AsString())
	assert.NotEqual(t, "_OTHER", errorType.AsString())
}
