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
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
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

func TestHTTPClientMetrics(t *testing.T) {
	// Reset initialization
	initOnce = *new(sync.Once)

	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp")

	// Setup tracer and meter
	sr, _ := setupTestTracer(t)

	// Setup meter with manual reader
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	// Create test request
	req, _ := http.NewRequest("POST", "http://example.com/api/data", nil)
	req.ContentLength = 100 // Request size
	
	// Create mock context
	mockCtx := hooktest.NewMockHookContext()
	transport := &http.Transport{}

	// Call BeforeRoundTrip
	BeforeRoundTrip(mockCtx, transport, req)

	// Get the span and context
	data, ok := mockCtx.GetData().(map[string]interface{})
	require.True(t, ok)
	_, ok = data["span"].(trace.Span)
	require.True(t, ok)

	// Create response
	response := &http.Response{
		StatusCode:    200,
		ContentLength: 500, // Response size
		Request:       req,
	}

	// Call AfterRoundTrip
	AfterRoundTrip(mockCtx, response, nil)

	// Verify span was ended
	spans := sr.Ended()
	require.Len(t, spans, 1)

	// Collect and verify metrics
	rm := &metricdata.ResourceMetrics{}
	err := reader.Collect(context.Background(), rm)
	require.NoError(t, err)

	// Extract metric names
	metricNames := make(map[string]bool)
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			metricNames[m.Name] = true
		}
	}

	// Verify expected metrics are present
	assert.True(t, metricNames["http.client.request.duration"], "request duration metric should be recorded")
	assert.True(t, metricNames["http.client.request.body.size"], "request body size metric should be recorded")
	assert.True(t, metricNames["http.client.response.body.size"], "response body size metric should be recorded")
}

func TestHTTPClientMetrics_ErrorCase(t *testing.T) {
	// Reset initialization
	initOnce = *new(sync.Once)

	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp")

	// Setup tracer and meter
	sr, _ := setupTestTracer(t)

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	// Create test request
	req, _ := http.NewRequest("GET", "http://example.com/path", nil)
	
	// Create mock context
	mockCtx := hooktest.NewMockHookContext()
	transport := &http.Transport{}

	// Call BeforeRoundTrip
	BeforeRoundTrip(mockCtx, transport, req)

	// Call AfterRoundTrip with error (no response)
	err := errors.New("connection timeout")
	AfterRoundTrip(mockCtx, nil, err)

	// Verify span was ended with error
	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status().Code)

	// Collect metrics - should have duration but not sizes
	rm := &metricdata.ResourceMetrics{}
	collectErr := reader.Collect(context.Background(), rm)
	require.NoError(t, collectErr)

	// With error and no response, we should not record metrics
	// (no response means no status code, size, etc.)
	// This is expected behavior
}

func TestHTTPClientMetrics_NilMeter(t *testing.T) {
	// Reset initialization
	initOnce = *new(sync.Once)

	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp")

	// Setup tracer but NO meter
	sr, _ := setupTestTracer(t)
	otel.SetMeterProvider(noopMeterProvider{})

	// Create test request
	req, _ := http.NewRequest("GET", "http://example.com/path", nil)
	
	// Create mock context
	mockCtx := hooktest.NewMockHookContext()
	transport := &http.Transport{}

	// Call BeforeRoundTrip
	BeforeRoundTrip(mockCtx, transport, req)

	// Create response
	response := &http.Response{
		StatusCode:    200,
		ContentLength: 100,
		Request:       req,
	}

	// Call AfterRoundTrip - should not panic even without metrics
	require.NotPanics(t, func() {
		AfterRoundTrip(mockCtx, response, nil)
	})

	// Verify span was still created and ended
	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Unset, spans[0].Status().Code)
}

// noopMeterProvider is a meter provider that returns a nil Meter, for testing
// graceful degradation when metrics are unavailable.
type noopMeterProvider struct {
	noop.MeterProvider
}

func (noopMeterProvider) Meter(name string, opts ...metric.MeterOption) metric.Meter {
	return nil
}
