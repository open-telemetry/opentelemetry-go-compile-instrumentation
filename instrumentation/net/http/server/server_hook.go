// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	otelsemconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"

	httpsemconv "go.opentelemetry.io/otelc/instrumentation/net/http/semconv"
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
	metrics    httpsemconv.HTTPServer
	initOnce   sync.Once
)

func initInstrumentation() {
	initOnce.Do(func() {
		version := runtime.ModuleVersion()
		tracer = otel.GetTracerProvider().Tracer(
			instrumentationName,
			trace.WithInstrumentationVersion(version),
		)
		propagator = otel.GetTextMapPropagator()
		meter := otel.GetMeterProvider().Meter(
			instrumentationName,
			metric.WithInstrumentationVersion(version),
			metric.WithSchemaURL(otelsemconv.SchemaURL),
		)
		metrics = httpsemconv.NewHTTPServer(meter)
		logger.Info("HTTP server instrumentation initialized")
	})
}

// netHttpServerEnabler controls whether server instrumentation is enabled
type netHttpServerEnabler struct{}

func (n netHttpServerEnabler) Enable() bool {
	return runtime.Instrumented(instrumentationKey)
}

var serverEnabler = netHttpServerEnabler{}

func BeforeServeHTTP(ictx hook.HookContext, recv interface{}, w http.ResponseWriter, r *http.Request) {
	if !serverEnabler.Enable() {
		logger.Debug("HTTP server instrumentation disabled")
		return
	}

	initInstrumentation()

	logger.Debug("BeforeServeHTTP called",
		"method", r.Method,
		"url", r.URL.String(),
		"remote_addr", r.RemoteAddr)

	// Extract trace context from incoming request headers
	ctx := propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))

	// Get trace attributes from semconv
	attrs := httpsemconv.HTTPServerRequestTraceAttrs("", r)

	// Get HTTP route from r.Pattern (Go 1.22+)
	route := httpsemconv.HTTPRoute(r.Pattern)
	spanName := httpsemconv.HTTPServerSpanName(r.Method, route)

	// Start span
	ctx, span := tracer.Start(ctx,
		spanName,
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(attrs...),
	)

	// Add route attribute if available
	if route != "" {
		span.SetAttributes(httpsemconv.HTTPServerRoute(route))
	}

	// Wrap ResponseWriter to capture status code
	wrapper := &writerWrapper{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}
	ictx.SetParam(responseWriterIndex, wrapper)

	// Update request with new context containing the span
	newReq := r.WithContext(ctx)
	ictx.SetParam(requestIndex, newReq)

	activeMetricAttrs := attribute.NewSet(metrics.ActiveRequestMetricAttributes("", r, nil)...)
	metrics.AddActiveRequests(ctx, 1, activeMetricAttrs)

	// Store data for after hook
	ictx.SetData(map[string]interface{}{
		"activeMetricAttrs": activeMetricAttrs,
		"ctx":               ctx,
		"req":               r,
		"route":             route,
		"span":              span,
		"start":             time.Now(),
	})
}

func AfterServeHTTP(ictx hook.HookContext) {
	if !serverEnabler.Enable() {
		return
	}

	ctx, _ := ictx.GetKeyData("ctx").(context.Context)
	if ctx == nil {
		ctx = context.Background()
	}
	if set, ok := ictx.GetKeyData("activeMetricAttrs").(attribute.Set); ok {
		defer metrics.AddActiveRequests(ctx, -1, set)
	}

	span, ok := ictx.GetKeyData("span").(trace.Span)
	if !ok || span == nil {
		logger.Debug("AfterServeHTTP: no span from before hook")
		return
	}
	defer span.End()

	// Extract status code from wrapped ResponseWriter
	statusCode := http.StatusOK
	responseSize := int64(0)
	if p, ok := ictx.GetParam(responseWriterIndex).(http.ResponseWriter); ok {
		if wrapper, ok := p.(*writerWrapper); ok {
			statusCode = wrapper.statusCode
			responseSize = wrapper.written
		}
	}

	// Add response attributes
	attrs := httpsemconv.HTTPServerResponseTraceAttrs(statusCode, responseSize)
	span.SetAttributes(attrs...)

	// Set span status based on status code
	code, desc := httpsemconv.HTTPServerStatus(statusCode)
	if code != codes.Unset {
		span.SetStatus(code, desc)
	}

	startTime, _ := ictx.GetKeyData("start").(time.Time)
	req, hasRequest := ictx.GetKeyData("req").(*http.Request)
	if !startTime.IsZero() && hasRequest {
		metricAttrs := []attribute.KeyValue(nil)
		if statusCode >= 500 && statusCode < 600 {
			metricAttrs = append(metricAttrs, otelsemconv.ErrorTypeKey.String(strconv.Itoa(statusCode)))
		}
		route, _ := ictx.GetKeyData("route").(string)
		metrics.RecordMetrics(
			ctx,
			"",
			req,
			statusCode,
			route,
			req.ContentLength,
			responseSize,
			time.Since(startTime).Seconds(),
			metricAttrs,
		)
	}
	logger.Debug("AfterServeHTTP called",
		"status_code", statusCode,
		"duration_ms", time.Since(startTime).Milliseconds())

	logger.Debug("AfterServeHTTP completed")
}
