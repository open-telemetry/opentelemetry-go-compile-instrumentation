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
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
)

type mockHookContext struct {
	params      map[int]interface{}
	returnVals  map[int]interface{}
	data        interface{}
	funcName    string
	packageName string
	skipCall    bool
}

func newMockHookContext() *mockHookContext {
	return &mockHookContext{
		params:      make(map[int]interface{}),
		returnVals:  make(map[int]interface{}),
		funcName:    "mockFunc",
		packageName: "mock",
	}
}

func (m *mockHookContext) SetSkipCall(skip bool) {
	m.skipCall = skip
}

func (m *mockHookContext) IsSkipCall() bool {
	return m.skipCall
}

func (m *mockHookContext) SetParam(index int, value interface{}) {
	m.params[index] = value
}

func (m *mockHookContext) GetParam(index int) interface{} {
	return m.params[index]
}

func (m *mockHookContext) GetParamCount() int {
	return len(m.params)
}

func (m *mockHookContext) SetReturnVal(index int, value interface{}) {
	m.returnVals[index] = value
}

func (m *mockHookContext) GetReturnVal(index int) interface{} {
	return m.returnVals[index]
}

func (m *mockHookContext) GetReturnValCount() int {
	return len(m.returnVals)
}

func (m *mockHookContext) SetData(data interface{}) {
	m.data = data
}

func (m *mockHookContext) GetData() interface{} {
	return m.data
}

func (m *mockHookContext) GetKeyData(key string) interface{} {
	if m.data == nil {
		return nil
	}
	dataMap, ok := m.data.(map[string]interface{})
	if !ok {
		return nil
	}
	return dataMap[key]
}

func (m *mockHookContext) SetKeyData(key string, val interface{}) {
	if m.data == nil {
		m.data = make(map[string]interface{})
	}
	dataMap, ok := m.data.(map[string]interface{})
	if !ok {
		m.data = make(map[string]interface{})
		dataMap = m.data.(map[string]interface{})
	}
	dataMap[key] = val
}

func (m *mockHookContext) HasKeyData(key string) bool {
	if m.data == nil {
		return false
	}
	dataMap, ok := m.data.(map[string]interface{})
	if !ok {
		return false
	}
	_, exists := dataMap[key]
	return exists
}

func (m *mockHookContext) GetFuncName() string {
	return m.funcName
}

func (m *mockHookContext) GetPackageName() string {
	return m.packageName
}

func setupTestTracer() (*tracetest.SpanRecorder, *sdktrace.TracerProvider) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
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
			sr, tp := setupTestTracer()
			defer tp.Shutdown(context.Background())

			req := tt.setupRequest()
			mockCtx := newMockHookContext()
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
		setupContext func(*sdktrace.TracerProvider) inst.HookContext
		response     *http.Response
		err          error
		validateSpan func(*testing.T, []sdktrace.ReadOnlySpan)
	}{
		{
			name: "successful response",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp")
			},
			setupContext: func(tp *sdktrace.TracerProvider) inst.HookContext {
				testTracer := tp.Tracer(instrumentationName)
				req, _ := http.NewRequest("GET", "http://example.com/path", nil)
				ctx, span := testTracer.Start(context.Background(), "GET", trace.WithSpanKind(trace.SpanKindClient))

				mockCtx := newMockHookContext()
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
			setupContext: func(tp *sdktrace.TracerProvider) inst.HookContext {
				testTracer := tp.Tracer(instrumentationName)
				req, _ := http.NewRequest("GET", "http://example.com/path", nil)
				ctx, span := testTracer.Start(context.Background(), "GET", trace.WithSpanKind(trace.SpanKindClient))

				mockCtx := newMockHookContext()
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
			setupContext: func(tp *sdktrace.TracerProvider) inst.HookContext {
				testTracer := tp.Tracer(instrumentationName)
				req, _ := http.NewRequest("GET", "http://example.com/path", nil)
				ctx, span := testTracer.Start(context.Background(), "GET", trace.WithSpanKind(trace.SpanKindClient))

				mockCtx := newMockHookContext()
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
			setupContext: func(tp *sdktrace.TracerProvider) inst.HookContext {
				testTracer := tp.Tracer(instrumentationName)
				req, _ := http.NewRequest("GET", "http://example.com/path", nil)
				ctx, span := testTracer.Start(context.Background(), "GET", trace.WithSpanKind(trace.SpanKindClient))

				mockCtx := newMockHookContext()
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
			setupContext: func(tp *sdktrace.TracerProvider) inst.HookContext {
				return newMockHookContext()
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
			setupContext: func(tp *sdktrace.TracerProvider) inst.HookContext {
				testTracer := tp.Tracer(instrumentationName)
				req, _ := http.NewRequest("GET", "http://example.com/path", nil)
				ctx, span := testTracer.Start(context.Background(), "GET", trace.WithSpanKind(trace.SpanKindClient))

				mockCtx := newMockHookContext()
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
			sr, tp := setupTestTracer()
			defer tp.Shutdown(context.Background())

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
