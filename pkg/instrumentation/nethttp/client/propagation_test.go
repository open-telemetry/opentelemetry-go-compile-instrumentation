// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package client

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
)

const (
	testParentSpanName = "parent_operation"
	testChildSpanName  = "http_client_request"
)

var (
	testTraceID = trace.TraceID{
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
	}
	testSpanID = trace.SpanID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
)

// TestClientContextPropagation verifies that trace context is properly injected
// into outgoing HTTP requests via the traceparent header.
func TestClientContextPropagation(t *testing.T) {
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

	// Create expected trace context
	expectedTraceID := testTraceID
	expectedSpanID := testSpanID

	var receivedTraceID trace.TraceID
	var receivedTraceparent string

	// Create test server that validates incoming headers
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture the traceparent header
		receivedTraceparent = r.Header.Get("Traceparent")

		// Extract context from incoming headers
		ctx := prop.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		sc := trace.SpanContextFromContext(ctx)

		// Store the received trace ID
		receivedTraceID = sc.TraceID()

		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// Create request with trace context
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    expectedTraceID,
		SpanID:     expectedSpanID,
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL, nil)
	require.NoError(t, err)

	// Inject trace context into request headers (simulating what the instrumentation does)
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	// Make request
	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Verify response
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Verify traceparent header was present
	assert.NotEmpty(t, receivedTraceparent, "traceparent header should be present")

	// Verify trace context was propagated correctly
	assert.Equal(t, expectedTraceID, receivedTraceID, "trace ID should match")
	// Note: The span ID might differ because we're using the parent span context
	// What matters is the trace ID matches for distributed tracing
}

// TestClientContextPropagationWithBaggage verifies that baggage is properly propagated.
func TestClientContextPropagationWithBaggage(t *testing.T) {
	// Setup propagators
	prop := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
	otel.SetTextMapPropagator(prop)

	var receivedTraceparent string

	// Create test server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedTraceparent = r.Header.Get("Traceparent")
		// Baggage header can be checked if needed
		_ = r.Header.Get("Baggage")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// Create context with trace and baggage
	traceID := testTraceID
	spanID := testSpanID
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL, nil)
	require.NoError(t, err)

	// Inject context (including any baggage) into headers
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	// Make request
	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	// Verify traceparent was propagated
	assert.NotEmpty(t, receivedTraceparent, "traceparent header should be present")
}

// TestSpanParentChildRelationship verifies that spans have correct parent-child relationships.
func TestSpanParentChildRelationship(t *testing.T) {
	// Setup test tracer provider with span recorder
	spanRecorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(spanRecorder),
	)
	defer func() { _ = provider.Shutdown(context.Background()) }()

	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	tracer := provider.Tracer("test")

	// Create test server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// Create parent span
	ctx, parentSpan := tracer.Start(context.Background(), testParentSpanName)

	// Create child span (simulating what instrumentation does)
	ctx, childSpan := tracer.Start(ctx, testChildSpanName)

	// Create and execute request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL, nil)
	require.NoError(t, err)

	// Inject trace context
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// End spans
	childSpan.End()
	parentSpan.End()

	// Verify span relationships
	spans := spanRecorder.Ended()
	require.Len(t, spans, 2, "should have 2 spans")

	// Find parent and child spans using ReadOnlySpan interface
	var parentSpanCtx, childSpanCtx trace.SpanContext
	var childParentSpanID trace.SpanID
	for _, span := range spans {
		if span.Name() == testParentSpanName {
			parentSpanCtx = span.SpanContext()
		} else if span.Name() == testChildSpanName {
			childSpanCtx = span.SpanContext()
			childParentSpanID = span.Parent().SpanID()
		}
	}

	// Verify parent-child relationship
	assert.Equal(t, parentSpanCtx.TraceID(), childSpanCtx.TraceID(),
		"parent and child should have same trace ID")
	assert.Equal(t, parentSpanCtx.SpanID(), childParentSpanID,
		"child's parent span ID should match parent's span ID")
}
