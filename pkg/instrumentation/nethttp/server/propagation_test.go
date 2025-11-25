// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	// Import client package to enable client-side instrumentation hooks
	_ "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/nethttp/client"
)

// TestServerContextExtraction verifies that trace context is properly extracted
// from incoming HTTP requests via the traceparent header.
func TestServerContextExtraction(t *testing.T) {
	// Setup test tracer provider with span recorder
	spanRecorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(spanRecorder),
	)
	defer func() { _ = provider.Shutdown(context.Background()) }()

	// Set global tracer provider and propagator
	otel.SetTracerProvider(provider)
	prop := propagation.TraceContext{}
	otel.SetTextMapPropagator(prop)

	// Create expected trace context (simulating upstream service)
	expectedTraceID := trace.TraceID{
		0x01,
		0x02,
		0x03,
		0x04,
		0x05,
		0x06,
		0x07,
		0x08,
		0x09,
		0x0a,
		0x0b,
		0x0c,
		0x0d,
		0x0e,
		0x0f,
		0x10,
	}
	expectedSpanID := trace.SpanID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}

	var extractedTraceID trace.TraceID
	var extractedParentSpanID trace.SpanID

	// Create test handler that extracts and uses context
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract context from incoming headers (simulating what instrumentation does)
		ctx := prop.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		sc := trace.SpanContextFromContext(ctx)

		// Store the extracted trace and parent span IDs
		extractedTraceID = sc.TraceID()
		extractedParentSpanID = sc.SpanID()

		w.WriteHeader(http.StatusOK)
	})

	// Create test server
	ts := httptest.NewServer(handler)
	defer ts.Close()

	// Create request with trace context in headers
	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	require.NoError(t, err)

	// Inject trace context into request headers (simulating upstream service)
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    expectedTraceID,
		SpanID:     expectedSpanID,
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	// Make request
	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Verify response
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Verify trace context was extracted correctly
	assert.Equal(t, expectedTraceID, extractedTraceID, "trace ID should match")
	assert.Equal(t, expectedSpanID, extractedParentSpanID, "parent span ID should match")
}

// TestServerCreatesChildSpan verifies that the server creates a child span
// when handling a request with trace context.
func TestServerCreatesChildSpan(t *testing.T) {
	// Setup test tracer provider with span recorder
	spanRecorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(spanRecorder),
	)
	defer func() { _ = provider.Shutdown(context.Background()) }()

	otel.SetTracerProvider(provider)
	prop := propagation.TraceContext{}
	otel.SetTextMapPropagator(prop)

	tracer := provider.Tracer("test-server")

	// Create expected trace context (simulating upstream service)
	expectedTraceID := trace.TraceID{
		0x01,
		0x02,
		0x03,
		0x04,
		0x05,
		0x06,
		0x07,
		0x08,
		0x09,
		0x0a,
		0x0b,
		0x0c,
		0x0d,
		0x0e,
		0x0f,
		0x10,
	}
	parentSpanID := trace.SpanID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}

	// Create test handler that creates a child span
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract context from incoming headers (simulating what instrumentation does)
		ctx := prop.Extract(r.Context(), propagation.HeaderCarrier(r.Header))

		// Create server span (simulating what instrumentation does)
		_, span := tracer.Start(ctx, "http_server_request")
		defer span.End()

		w.WriteHeader(http.StatusOK)
	})

	// Create test server
	ts := httptest.NewServer(handler)
	defer ts.Close()

	// Create request with trace context in headers
	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	require.NoError(t, err)

	// Inject trace context into request headers (simulating upstream service)
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    expectedTraceID,
		SpanID:     parentSpanID,
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	// Make request
	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Verify span was created
	spans := spanRecorder.Ended()
	require.Len(t, spans, 1, "should have 1 span")

	serverSpan := spans[0]
	assert.Equal(t, "http_server_request", serverSpan.Name())
	assert.Equal(t, expectedTraceID, serverSpan.SpanContext().TraceID(),
		"server span should have same trace ID as parent")
	assert.Equal(t, parentSpanID, serverSpan.Parent().SpanID(),
		"server span's parent should be the incoming span")
}

// TestServerNoContextCreatesRootSpan verifies that when no trace context
// is provided, the server creates a root span.
func TestServerNoContextCreatesRootSpan(t *testing.T) {
	// Setup test tracer provider with span recorder
	spanRecorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(spanRecorder),
	)
	defer func() { _ = provider.Shutdown(context.Background()) }()

	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	tracer := provider.Tracer("test-server")

	// Create test handler that creates a span
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract context (should be empty since no traceparent header)
		ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))

		// Create server span
		_, span := tracer.Start(ctx, "http_server_request")
		defer span.End()

		w.WriteHeader(http.StatusOK)
	})

	// Create test server
	ts := httptest.NewServer(handler)
	defer ts.Close()

	// Create request WITHOUT trace context
	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	require.NoError(t, err)

	// Make request
	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Verify span was created
	spans := spanRecorder.Ended()
	require.Len(t, spans, 1, "should have 1 span")

	serverSpan := spans[0]
	assert.Equal(t, "http_server_request", serverSpan.Name())
	// Parent should be empty for root span
	assert.False(t, serverSpan.Parent().IsValid(),
		"root span should not have a valid parent")
}

// TestServerDistributedTracing verifies end-to-end distributed tracing scenario.
func TestServerDistributedTracing(t *testing.T) {
	// Setup test tracer provider with span recorder
	spanRecorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(spanRecorder),
	)
	defer func() { _ = provider.Shutdown(context.Background()) }()

	otel.SetTracerProvider(provider)
	prop := propagation.TraceContext{}
	otel.SetTextMapPropagator(prop)

	tracer := provider.Tracer("test")

	// Create test server that creates spans and makes downstream calls
	var downstreamCalled bool
	var downstreamTraceID trace.TraceID

	// Downstream service
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downstreamCalled = true
		ctx := prop.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		sc := trace.SpanContextFromContext(ctx)
		downstreamTraceID = sc.TraceID()
		w.WriteHeader(http.StatusOK)
	}))
	defer downstream.Close()

	// Main service that calls downstream
	mainService := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract incoming context
		ctx := prop.Extract(r.Context(), propagation.HeaderCarrier(r.Header))

		// Create server span
		ctx, span := tracer.Start(ctx, "main_service")
		defer span.End()

		// Make downstream call
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, downstream.URL, nil)
		prop.Inject(ctx, propagation.HeaderCarrier(req.Header))

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()

		w.WriteHeader(http.StatusOK)
	}))
	defer mainService.Close()

	// Create initial context (simulating entry point)
	ctx, rootSpan := tracer.Start(context.Background(), "entry_point")

	// Make request to main service
	req, err := http.NewRequest(http.MethodGet, mainService.URL, nil)
	require.NoError(t, err)
	prop.Inject(ctx, propagation.HeaderCarrier(req.Header))

	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	rootSpan.End()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.True(t, downstreamCalled, "downstream service should have been called")

	// Verify trace ID propagated through all services
	spans := spanRecorder.Ended()
	require.Len(t, spans, 2, "should have 2 spans (entry_point and main_service)")

	// Get root span's trace ID
	var rootTraceID trace.TraceID
	for _, span := range spans {
		if span.Name() == "entry_point" {
			rootTraceID = span.SpanContext().TraceID()
			break
		}
	}

	// All spans should have the same trace ID
	for _, span := range spans {
		assert.Equal(t, rootTraceID, span.SpanContext().TraceID(),
			"all spans should have the same trace ID")
	}

	// Downstream service should have received the same trace ID
	assert.Equal(t, rootTraceID, downstreamTraceID,
		"downstream service should have received the same trace ID")
}
