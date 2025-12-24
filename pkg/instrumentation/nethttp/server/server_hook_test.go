// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
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

func TestBeforeServeHTTP(t *testing.T) {
	tests := []struct {
		name            string
		setupEnv        func(t *testing.T)
		setupRequest    func() *http.Request
		expectSpan      bool
		validateSpan    func(*testing.T, trace.Span)
		validateWriter  func(*testing.T, http.ResponseWriter)
		validateContext func(*testing.T, *http.Request)
	}{
		{
			name: "basic request creates span",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp")
			},
			setupRequest: func() *http.Request {
				return httptest.NewRequest("GET", "http://example.com/path", nil)
			},
			expectSpan: true,
			validateSpan: func(t *testing.T, span trace.Span) {
				assert.NotNil(t, span)
			},
			validateWriter: func(t *testing.T, w http.ResponseWriter) {
				_, ok := w.(*writerWrapper)
				assert.True(t, ok, "writer should be wrapped")
			},
		},
		{
			name: "instrumentation disabled",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "nethttp")
			},
			setupRequest: func() *http.Request {
				return httptest.NewRequest("GET", "http://example.com/path", nil)
			},
			expectSpan: false,
		},
		{
			name: "POST request",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp")
			},
			setupRequest: func() *http.Request {
				return httptest.NewRequest("POST", "http://example.com/api/data", nil)
			},
			expectSpan: true,
		},
		{
			name: "request with trace context propagation",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp")
			},
			setupRequest: func() *http.Request {
				req := httptest.NewRequest("GET", "http://example.com/path", nil)
				// Add traceparent header to simulate incoming trace
				req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0bb902b7-01")
				return req
			},
			expectSpan: true,
			validateContext: func(t *testing.T, req *http.Request) {
				// Context should have extracted trace
				spanCtx := trace.SpanContextFromContext(req.Context())
				assert.True(t, spanCtx.IsValid())
			},
		},
		{
			name: "request with route pattern (Go 1.22+)",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp")
			},
			setupRequest: func() *http.Request {
				req := httptest.NewRequest("GET", "http://example.com/users/123", nil)
				req.SetPathValue("id", "123")
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
			w := httptest.NewRecorder()
			mockCtx := newMockHookContext()

			BeforeServeHTTP(mockCtx, nil, w, req)

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

				// Check that ResponseWriter was wrapped
				wrappedWriter, ok := mockCtx.GetParam(1).(http.ResponseWriter)
				require.True(t, ok, "param 1 should be ResponseWriter")
				require.NotNil(t, wrappedWriter, "wrapped writer should not be nil")

				if tt.validateWriter != nil {
					tt.validateWriter(t, wrappedWriter)
				}

				if tt.validateContext != nil {
					// Get the updated request with the new context
					updatedReq, ok := mockCtx.GetParam(2).(*http.Request)
					require.True(t, ok, "param 2 should be updated request")
					tt.validateContext(t, updatedReq)
				}
			} else {
				// No span should be created
				data := mockCtx.GetData()
				assert.Nil(t, data, "no data should be stored when instrumentation disabled")
			}
		})
	}
}

func TestAfterServeHTTP(t *testing.T) {
	tests := []struct {
		name         string
		setupEnv     func(t *testing.T)
		setupContext func(*sdktrace.TracerProvider) inst.HookContext
		statusCode   int
		validateSpan func(*testing.T, []sdktrace.ReadOnlySpan)
	}{
		{
			name: "successful 200 response",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp")
			},
			setupContext: func(tp *sdktrace.TracerProvider) inst.HookContext {
				testTracer := tp.Tracer(instrumentationName)
				ctx, span := testTracer.Start(
					context.Background(),
					"GET /path",
					trace.WithSpanKind(trace.SpanKindServer),
				)

				mockCtx := newMockHookContext()
				wrapper := &writerWrapper{
					ResponseWriter: httptest.NewRecorder(),
					statusCode:     200,
				}
				mockCtx.SetParam(1, wrapper)
				mockCtx.SetData(map[string]interface{}{
					"ctx":  ctx,
					"span": span,
				})
				return mockCtx
			},
			statusCode: 200,
			validateSpan: func(t *testing.T, spans []sdktrace.ReadOnlySpan) {
				require.Len(t, spans, 1)
				span := spans[0]
				assert.Equal(t, codes.Unset, span.Status().Code)
			},
		},
		{
			name: "404 not found",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp")
			},
			setupContext: func(tp *sdktrace.TracerProvider) inst.HookContext {
				testTracer := tp.Tracer(instrumentationName)
				ctx, span := testTracer.Start(
					context.Background(),
					"GET /notfound",
					trace.WithSpanKind(trace.SpanKindServer),
				)

				mockCtx := newMockHookContext()
				wrapper := &writerWrapper{
					ResponseWriter: httptest.NewRecorder(),
					statusCode:     404,
				}
				mockCtx.SetParam(1, wrapper)
				mockCtx.SetData(map[string]interface{}{
					"ctx":  ctx,
					"span": span,
				})
				return mockCtx
			},
			statusCode: 404,
			validateSpan: func(t *testing.T, spans []sdktrace.ReadOnlySpan) {
				require.Len(t, spans, 1)
				span := spans[0]
				// 404 is not an error from OTel perspective for servers
				assert.Equal(t, codes.Unset, span.Status().Code)
			},
		},
		{
			name: "500 internal server error",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp")
			},
			setupContext: func(tp *sdktrace.TracerProvider) inst.HookContext {
				testTracer := tp.Tracer(instrumentationName)
				ctx, span := testTracer.Start(
					context.Background(),
					"GET /error",
					trace.WithSpanKind(trace.SpanKindServer),
				)

				mockCtx := newMockHookContext()
				wrapper := &writerWrapper{
					ResponseWriter: httptest.NewRecorder(),
					statusCode:     500,
				}
				mockCtx.SetParam(1, wrapper)
				mockCtx.SetData(map[string]interface{}{
					"ctx":  ctx,
					"span": span,
				})
				return mockCtx
			},
			statusCode: 500,
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
			statusCode: 200,
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
				ctx, span := testTracer.Start(
					context.Background(),
					"GET /path",
					trace.WithSpanKind(trace.SpanKindServer),
				)

				mockCtx := newMockHookContext()
				wrapper := &writerWrapper{
					ResponseWriter: httptest.NewRecorder(),
					statusCode:     200,
				}
				mockCtx.SetParam(1, wrapper)
				mockCtx.SetData(map[string]interface{}{
					"ctx":  ctx,
					"span": span,
				})
				return mockCtx
			},
			statusCode: 200,
			validateSpan: func(t *testing.T, spans []sdktrace.ReadOnlySpan) {
				// Span should not be ended because instrumentation is disabled
				assert.Equal(t, 0, len(spans))
			},
		},
		{
			name: "no wrapper in context",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp")
			},
			setupContext: func(tp *sdktrace.TracerProvider) inst.HookContext {
				testTracer := tp.Tracer(instrumentationName)
				ctx, span := testTracer.Start(
					context.Background(),
					"GET /path",
					trace.WithSpanKind(trace.SpanKindServer),
				)

				mockCtx := newMockHookContext()
				// Don't set param 1, defaults to 200
				mockCtx.SetData(map[string]interface{}{
					"ctx":  ctx,
					"span": span,
				})
				return mockCtx
			},
			statusCode: 200,
			validateSpan: func(t *testing.T, spans []sdktrace.ReadOnlySpan) {
				require.Len(t, spans, 1)
				// Should still work with default 200
				span := spans[0]
				assert.Equal(t, codes.Unset, span.Status().Code)
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

			AfterServeHTTP(mockCtx)

			spans := sr.Ended()
			if tt.validateSpan != nil {
				tt.validateSpan(t, spans)
			}
		})
	}
}

func TestServerEnabler(t *testing.T) {
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

			enabler := netHttpServerEnabler{}
			result := enabler.Enable()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestWriterWrapper_IntegrationWithHandler(t *testing.T) {
	tests := []struct {
		name               string
		handler            http.HandlerFunc
		expectedStatusCode int
		expectedBody       string
	}{
		{
			name: "handler that sets status",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusCreated)
				w.Write([]byte("created"))
			},
			expectedStatusCode: http.StatusCreated,
			expectedBody:       "created",
		},
		{
			name: "handler that only writes",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("ok"))
			},
			expectedStatusCode: http.StatusOK,
			expectedBody:       "ok",
		},
		{
			name: "handler that returns error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "internal error", http.StatusInternalServerError)
			},
			expectedStatusCode: http.StatusInternalServerError,
			expectedBody:       "internal error\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			wrapper := &writerWrapper{
				ResponseWriter: rec,
				statusCode:     http.StatusOK,
			}

			req := httptest.NewRequest("GET", "/test", nil)
			tt.handler(wrapper, req)

			assert.Equal(t, tt.expectedStatusCode, wrapper.statusCode)
			assert.Equal(t, tt.expectedBody, rec.Body.String())
		})
	}
}
