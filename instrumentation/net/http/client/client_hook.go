// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"

	httpsemconv "go.opentelemetry.io/otelc/instrumentation/net/http/semconv"
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
	meter      metric.Meter
	httpClient httpsemconv.HTTPClient
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
		meter = otel.GetMeterProvider().Meter(
			instrumentationName,
			metric.WithInstrumentationVersion(version),
			metric.WithSchemaURL(semconv.SchemaURL),
		)
		httpClient = httpsemconv.NewHTTPClient(meter)
		logger.Info("HTTP client instrumentation initialized with metrics")
	})
}

// netHttpClientEnabler controls whether client instrumentation is enabled
type netHttpClientEnabler struct{}

func (n netHttpClientEnabler) Enable() bool {
	return runtime.Instrumented(instrumentationKey)
}

var clientEnabler = netHttpClientEnabler{}

func BeforeRoundTrip(ictx hook.HookContext, transport *http.Transport, req *http.Request) {
	if !clientEnabler.Enable() {
		logger.Debug("HTTP client instrumentation disabled")
		return
	}

	if runtime.IsHTTPClientInstrumentationSuppressed(req.Context()) {
		return
	}

	// Filter out OTel exporter requests to prevent infinite loops
	ua := req.Header.Get("User-Agent")
	if strings.HasPrefix(ua, otelExporterPrefix) || strings.HasPrefix(ua, "OTel Go OTLP") ||
		strings.HasPrefix(ua, "OTel-Go-OTLP") {
		logger.Debug("Skipping OTel exporter request", "user_agent", ua)
		return
	}

	initInstrumentation()

	logger.Debug("BeforeRoundTrip called",
		"method", req.Method,
		"url", req.URL.String(),
		"host", req.Host)

	ctx := req.Context()

	// Get trace attributes from semconv
	attrs := httpClient.RequestTraceAttrs(req)

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
	requestSize := req.ContentLength
	ictx.SetData(map[string]interface{}{
		"ctx":          ctx,
		"span":         span,
		"req":          req,
		"start":        time.Now(),
		"request_size": requestSize,
	})
}

func AfterRoundTrip(ictx hook.HookContext, res *http.Response, err error) {
	if !clientEnabler.Enable() {
		logger.Debug("HTTP client instrumentation disabled")
		return
	}

	span, ok := ictx.GetKeyData("span").(trace.Span)
	if !ok || span == nil {
		logger.Debug("AfterRoundTrip: no span from before hook")
		return
	}
	defer span.End()

	// Add response attributes
	if res != nil {
		ctx, _ := ictx.GetKeyData("ctx").(context.Context)
		startTime, _ := ictx.GetKeyData("start").(time.Time)
		requestSize, _ := ictx.GetKeyData("request_size").(int64)
		req, _ := ictx.GetKeyData("req").(*http.Request)

		attrs := httpClient.ResponseTraceAttrs(res)
		span.SetAttributes(attrs...)

		// Set span status based on status code
		code, desc := httpClient.Status(res.StatusCode)
		if code != codes.Unset {
			span.SetStatus(code, desc)
		}

		// Record metrics
		if ctx != nil && req != nil {
			responseSize := res.ContentLength
			elapsedTime := time.Since(startTime).Seconds()
			httpClient.RecordMetrics(ctx, req, res.StatusCode, requestSize, responseSize, elapsedTime, nil)
		}

		logger.Debug("AfterRoundTrip called",
			"method", res.Request.Method,
			"url", res.Request.URL.String(),
			"status_code", res.StatusCode,
			"duration_ms", time.Since(startTime).Milliseconds())
	}

	// Handle error
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.SetAttributes(httpClient.ErrorType(err))
		logger.Debug("AfterRoundTrip called with error", "error", err)
	}

	logger.Debug("AfterRoundTrip completed")
}
