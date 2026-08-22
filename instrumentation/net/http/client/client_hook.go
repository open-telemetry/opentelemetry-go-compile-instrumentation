// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
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
	otelExporterPrefix  = "OTel OTLP Exporter Go"
	instrumentationName = "go.opentelemetry.io/otelc/instrumentation/net/http"
	instrumentationKey  = "NETHTTP"
	requestParamIndex   = 1
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
		logger.Info("HTTP client instrumentation initialized")
	})
}

// debugEnabled gates per-request Debug calls: slog evaluates arguments before
// checking the level, so an unguarded call pays for req.URL.String() and
// attribute boxing on every request even when debug logging is off.
func debugEnabled() bool {
	return logger.Enabled(context.Background(), slog.LevelDebug)
}

// netHttpClientEnabler controls whether client instrumentation is enabled
type netHttpClientEnabler struct{}

func (n netHttpClientEnabler) Enable() bool {
	return runtime.Instrumented(instrumentationKey)
}

var clientEnabler = netHttpClientEnabler{}

// hookData carries span state from BeforeRoundTrip to AfterRoundTrip. A typed
// struct instead of SetKeyData's map[string]interface{} keeps the per-request
// cost to a single small allocation with no map or string hashing.
type hookData struct {
	span  trace.Span
	start time.Time
}

func BeforeRoundTrip(ictx hook.HookContext, transport *http.Transport, req *http.Request) {
	// This runs once per outbound request; keep the disabled path free of logging.
	if !clientEnabler.Enable() {
		return
	}

	if runtime.IsHTTPClientInstrumentationSuppressed(req.Context()) {
		return
	}

	// Filter out OTel exporter requests to prevent infinite loops
	ua := req.Header.Get("User-Agent")
	if strings.HasPrefix(ua, otelExporterPrefix) || strings.HasPrefix(ua, "OTel Go OTLP") ||
		strings.HasPrefix(ua, "OTel-Go-OTLP") {
		if debugEnabled() {
			logger.Debug("Skipping OTel exporter request", "user_agent", ua)
		}
		return
	}

	initInstrumentation()

	if debugEnabled() {
		logger.Debug("BeforeRoundTrip called",
			"method", req.Method,
			"url", req.URL.String(),
			"host", req.Host)
	}

	ctx := req.Context()

	// Get trace attributes from semconv
	attrs := semconv.HTTPClientRequestTraceAttrs(req)

	// Start span
	spanName := req.Method
	ctx, span := tracer.Start(ctx,
		spanName,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrs...),
	)

	// Inject trace context into request headers
	propagator.Inject(ctx, propagation.HeaderCarrier(req.Header))

	// Update request with new context
	newReq := req.WithContext(ctx)
	ictx.SetParam(requestParamIndex, newReq)

	// Store data for after hook
	ictx.SetData(&hookData{
		span:  span,
		start: time.Now(),
	})
}

func AfterRoundTrip(ictx hook.HookContext, res *http.Response, err error) {
	data, ok := ictx.GetData().(*hookData)
	if !ok || data == nil || data.span == nil {
		logger.Debug("AfterRoundTrip: no span from before hook")
		return
	}
	span := data.span
	defer span.End()

	// Add response attributes
	if res != nil {
		attrs := semconv.HTTPClientResponseTraceAttrs(res)
		span.SetAttributes(attrs...)

		// Set span status based on status code
		code, desc := semconv.HTTPClientStatus(res.StatusCode)
		if code != codes.Unset {
			span.SetStatus(code, desc)
		}

		if debugEnabled() {
			logger.Debug("AfterRoundTrip called",
				"method", res.Request.Method,
				"url", res.Request.URL.String(),
				"status_code", res.StatusCode,
				"duration_ms", time.Since(data.start).Milliseconds())
		}
	}

	// Handle error
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.SetAttributes(semconv.HTTPClientErrorType(err))
		logger.Debug("AfterRoundTrip called with error", "error", err)
	}
}
