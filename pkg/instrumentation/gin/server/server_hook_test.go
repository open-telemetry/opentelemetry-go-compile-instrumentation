// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	gosync "sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst/insttest"
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

func TestBeforeServeHTTP(t *testing.T) {
	tests := []struct {
		name            string
		setupEnv        func(t *testing.T)
		setupRequest    func() *http.Request
		expectSpan      bool
		validateWriter  func(*testing.T, http.ResponseWriter)
		validateContext func(*testing.T, *http.Request)
	}{
		{
			name: "basic request creates span",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "gin")
			},
			setupRequest: func() *http.Request {
				return httptest.NewRequest(http.MethodGet, "http://example.com/path", nil)
			},
			expectSpan: true,
			validateWriter: func(t *testing.T, w http.ResponseWriter) {
				_, ok := w.(*writerWrapper)
				assert.True(t, ok, "writer should be wrapped")
			},
		},
		{
			name: "instrumentation disabled",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "gin")
			},
			setupRequest: func() *http.Request {
				return httptest.NewRequest(http.MethodGet, "http://example.com/path", nil)
			},
			expectSpan: false,
		},
		{
			name: "POST request creates span",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "gin")
			},
			setupRequest: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "http://example.com/api/data", nil)
			},
			expectSpan: true,
		},
		{
			name: "request with incoming trace context is linked",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "gin")
			},
			setupRequest: func() *http.Request {
				req := httptest.NewRequest(http.MethodGet, "http://example.com/path", nil)
				req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0bb902b7-01")
				return req
			},
			expectSpan: true,
			validateContext: func(t *testing.T, req *http.Request) {
				spanCtx := trace.SpanContextFromContext(req.Context())
				assert.True(t, spanCtx.IsValid())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initOnce = gosync.Once{}
			tt.setupEnv(t)
			sr, _ := setupTestTracer(t)

			req := tt.setupRequest()
			w := httptest.NewRecorder()
			mockCtx := insttest.NewMockHookContext()

			BeforeServeHTTP(mockCtx, nil, w, req)

			if tt.expectSpan {
				assert.Equal(t, 0, len(sr.Ended()), "span should not be ended in Before hook")

				data, ok := mockCtx.GetData().(map[string]interface{})
				require.True(t, ok, "data should be stored")
				require.NotNil(t, data)

				span, ok := data["span"].(trace.Span)
				require.True(t, ok, "span should be stored")
				require.NotNil(t, span)

				wrappedWriter, ok := mockCtx.GetParam(responseWriterIndex).(http.ResponseWriter)
				require.True(t, ok, "param 1 should be ResponseWriter")
				require.NotNil(t, wrappedWriter)

				if tt.validateWriter != nil {
					tt.validateWriter(t, wrappedWriter)
				}

				if tt.validateContext != nil {
					updatedReq, ok := mockCtx.GetParam(requestIndex).(*http.Request)
					require.True(t, ok, "param 2 should be updated request")
					tt.validateContext(t, updatedReq)
				}
			} else {
				assert.Nil(t, mockCtx.GetData(), "no data should be stored when instrumentation is disabled")
			}
		})
	}
}

func TestAfterServeHTTP(t *testing.T) {
	tests := []struct {
		name         string
		setupEnv     func(t *testing.T)
		setupContext func(*sdktrace.TracerProvider) *insttest.MockHookContext
		validateSpan func(*testing.T, []sdktrace.ReadOnlySpan)
	}{
		{
			name: "200 response sets Unset span status",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "gin")
			},
			setupContext: func(tp *sdktrace.TracerProvider) *insttest.MockHookContext {
				_, span := tp.Tracer(instrumentationName).Start(context.Background(), "GET",
					trace.WithSpanKind(trace.SpanKindServer))
				mockCtx := insttest.NewMockHookContext()
				mockCtx.SetParam(responseWriterIndex, &writerWrapper{ResponseWriter: httptest.NewRecorder(), statusCode: http.StatusOK})
				mockCtx.SetData(map[string]interface{}{"span": span})
				return mockCtx
			},
			validateSpan: func(t *testing.T, spans []sdktrace.ReadOnlySpan) {
				require.Len(t, spans, 1)
				assert.Equal(t, codes.Unset, spans[0].Status().Code)
			},
		},
		{
			name: "500 response sets Error span status",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "gin")
			},
			setupContext: func(tp *sdktrace.TracerProvider) *insttest.MockHookContext {
				_, span := tp.Tracer(instrumentationName).Start(context.Background(), "GET",
					trace.WithSpanKind(trace.SpanKindServer))
				mockCtx := insttest.NewMockHookContext()
				mockCtx.SetParam(responseWriterIndex, &writerWrapper{ResponseWriter: httptest.NewRecorder(), statusCode: http.StatusInternalServerError})
				mockCtx.SetData(map[string]interface{}{"span": span})
				return mockCtx
			},
			validateSpan: func(t *testing.T, spans []sdktrace.ReadOnlySpan) {
				require.Len(t, spans, 1)
				assert.Equal(t, codes.Error, spans[0].Status().Code)
			},
		},
		{
			name: "no span in context does nothing",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "gin")
			},
			setupContext: func(_ *sdktrace.TracerProvider) *insttest.MockHookContext {
				return insttest.NewMockHookContext()
			},
			validateSpan: func(t *testing.T, spans []sdktrace.ReadOnlySpan) {
				assert.Empty(t, spans)
			},
		},
		{
			name: "instrumentation disabled skips span end",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "gin")
			},
			setupContext: func(tp *sdktrace.TracerProvider) *insttest.MockHookContext {
				_, span := tp.Tracer(instrumentationName).Start(context.Background(), "GET",
					trace.WithSpanKind(trace.SpanKindServer))
				mockCtx := insttest.NewMockHookContext()
				mockCtx.SetParam(responseWriterIndex, &writerWrapper{ResponseWriter: httptest.NewRecorder(), statusCode: http.StatusOK})
				mockCtx.SetData(map[string]interface{}{"span": span})
				return mockCtx
			},
			validateSpan: func(t *testing.T, spans []sdktrace.ReadOnlySpan) {
				assert.Empty(t, spans)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initOnce = gosync.Once{}
			tt.setupEnv(t)
			sr, tp := setupTestTracer(t)

			mockCtx := tt.setupContext(tp)
			AfterServeHTTP(mockCtx)

			if tt.validateSpan != nil {
				tt.validateSpan(t, sr.Ended())
			}
		})
	}
}

func TestWriterWrapper_CapturesStatusCode(t *testing.T) {
	tests := []struct {
		name           string
		handler        http.HandlerFunc
		expectedStatus int
	}{
		{
			name:           "explicit WriteHeader",
			handler:        func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) },
			expectedStatus: http.StatusCreated,
		},
		{
			name:           "implicit 200 on Write",
			handler:        func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) },
			expectedStatus: http.StatusOK,
		},
		{
			name:           "error response",
			handler:        func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "err", http.StatusBadRequest) },
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			wrapper := &writerWrapper{ResponseWriter: rec}
			tt.handler(wrapper, httptest.NewRequest(http.MethodGet, "/", nil))
			assert.Equal(t, tt.expectedStatus, wrapper.statusCode)
		})
	}
}
