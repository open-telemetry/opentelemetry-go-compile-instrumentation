// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otelc/instrumentation/net/http/semconv"
	"go.opentelemetry.io/otelc/pkg/hook"
	"go.opentelemetry.io/otelc/pkg/runtime"
)

const (
	instrumentationName = "go.opentelemetry.io/otelc/instrumentation/net/http"
	instrumentationKey  = "NETHTTP"
	responseWriterIndex = 1
	requestIndex        = 2
)

var (
	logger     = runtime.Logger()
	tracer     trace.Tracer
	propagator propagation.TextMapPropagator
	initOnce   sync.Once
)

func initInstrumentation() {
	initOnce.Do(func() {
		tracer = otel.GetTracerProvider().Tracer(
			instrumentationName,
			trace.WithInstrumentationVersion(runtime.ModuleVersion()),
		)
		propagator = otel.GetTextMapPropagator()
		logger.Info("HTTP server instrumentation initialized")
	})
}

// debugEnabled gates per-request Debug calls: slog evaluates arguments before
// checking the level, so an unguarded call pays for r.URL.String() and
// attribute boxing on every request even when debug logging is off.
func debugEnabled() bool {
	return logger.Enabled(context.Background(), slog.LevelDebug)
}

// netHttpServerEnabler controls whether server instrumentation is enabled
type netHttpServerEnabler struct{}

func (n netHttpServerEnabler) Enable() bool {
	return runtime.Instrumented(instrumentationKey)
}

var serverEnabler = netHttpServerEnabler{}

// hookData carries span state from BeforeServeHTTP to AfterServeHTTP. A typed
// struct instead of SetKeyData's map[string]interface{} keeps the per-request
// cost to a single small allocation with no map or string hashing.
type hookData struct {
	span  trace.Span
	start time.Time
}

func BeforeServeHTTP(ictx hook.HookContext, recv interface{}, w http.ResponseWriter, r *http.Request) {
	// This runs once per request; keep the disabled path free of logging.
	if !serverEnabler.Enable() {
		return
	}

	initInstrumentation()

	if debugEnabled() {
		logger.Debug("BeforeServeHTTP called",
			"method", r.Method,
			"url", r.URL.String(),
			"remote_addr", r.RemoteAddr)
	}

	// Extract trace context from incoming request headers
	ctx := propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))

	// Get trace attributes from semconv
	attrs := semconv.HTTPServerRequestTraceAttrs("", r)

	// Route isn't known until ServeMux matches. AfterServeHTTP renames the span.
	spanName := semconv.HTTPServerSpanName(r.Method, "")

	// Start span
	ctx, span := tracer.Start(ctx,
		spanName,
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(attrs...),
	)

	// Wrap ResponseWriter to capture status code
	wrapper := &writerWrapper{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}
	ictx.SetParam(responseWriterIndex, wrapper)

	// Update request with new context containing the span
	newReq := r.WithContext(ctx)
	ictx.SetParam(requestIndex, newReq)

	// Store data for after hook
	ictx.SetData(&hookData{
		span:  span,
		start: time.Now(),
	})
}

func AfterServeHTTP(ictx hook.HookContext) {
	data, ok := ictx.GetData().(*hookData)
	if !ok || data == nil || data.span == nil {
		logger.Debug("AfterServeHTTP: no span from before hook")
		return
	}
	span := data.span
	defer span.End()

	// ServeMux fills in r.Pattern on the same request after the span was created.
	// Stays empty for routers that name the span themselves (gin, chi).
	if r, ok := ictx.GetParam(requestIndex).(*http.Request); ok && r != nil && span.IsRecording() {
		if route := semconv.HTTPRoute(r.Pattern); route != "" {
			span.SetName(semconv.HTTPServerSpanName(r.Method, route))
			span.SetAttributes(semconv.HTTPServerRoute(route))
		}
	}

	// Extract status code from wrapped ResponseWriter
	statusCode := http.StatusOK
	if p, ok := ictx.GetParam(responseWriterIndex).(http.ResponseWriter); ok {
		if wrapper, ok := p.(*writerWrapper); ok {
			statusCode = wrapper.statusCode
		}
	}

	// Add response attributes
	attrs := semconv.HTTPServerResponseTraceAttrs(statusCode, 0)
	span.SetAttributes(attrs...)

	// Set span status based on status code
	code, desc := semconv.HTTPServerStatus(statusCode)
	if code != codes.Unset {
		span.SetStatus(code, desc)
	}

	if debugEnabled() {
		logger.Debug("AfterServeHTTP called",
			"status_code", statusCode,
			"duration_ms", time.Since(data.start).Milliseconds())
	}
}
