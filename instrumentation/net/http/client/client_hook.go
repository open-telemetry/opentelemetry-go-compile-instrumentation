// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
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
	otelExporterPrefix  = "OTel OTLP Exporter Go"
	instrumentationName = "go.opentelemetry.io/otelc/instrumentation/net/http"
	instrumentationKey  = "NETHTTP"
	requestParamIndex   = 1
)

var (
	logger     = runtime.Logger()
	tracer     trace.Tracer
	propagator propagation.TextMapPropagator
	metrics    httpsemconv.HTTPClient
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
		metrics = httpsemconv.NewHTTPClient(meter)
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
	ctx               context.Context
	req               *http.Request
	span              trace.Span
	start             time.Time
	activeMetricAttrs attribute.Set
}

func BeforeRoundTrip(ictx hook.HookContext, transport *http.Transport, req *http.Request) {
	// This runs once per outbound request; keep the disabled path free of logging.
	if !clientEnabler.Enable() {
		return
	}

	if req == nil {
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
		var urlStr string
		if req.URL != nil {
			urlStr = req.URL.String()
		}
		logger.Debug("BeforeRoundTrip called",
			"method", req.Method,
			"url", urlStr,
			"host", req.Host)
	}

	ctx := req.Context()

	// Get trace attributes from semconv
	attrs := httpsemconv.HTTPClientRequestTraceAttrs(req)

	// Start span
	ctx, span := tracer.Start(ctx,
		httpsemconv.HTTPClientSpanName(req.Method),
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrs...),
	)

	// Ensure headers map is initialized before injecting trace context
	if req.Header == nil {
		req.Header = make(http.Header)
	}

	// Inject trace context into request headers
	propagator.Inject(ctx, propagation.HeaderCarrier(req.Header))

	// Update request with new context
	newReq := req.WithContext(ctx)
	ictx.SetParam(requestParamIndex, newReq)

	activeMetricAttrs := attribute.NewSet(metrics.ActiveRequestMetricAttributes(req, nil)...)
	metrics.AddActiveRequests(ctx, 1, activeMetricAttrs)

	// Store data for after hook
	ictx.SetData(&hookData{
		ctx:               ctx,
		req:               req,
		span:              span,
		start:             time.Now(),
		activeMetricAttrs: activeMetricAttrs,
	})
}

func AfterRoundTrip(ictx hook.HookContext, res *http.Response, err error) {
	data, ok := ictx.GetData().(*hookData)
	if !ok || data == nil || data.span == nil {
		logger.Debug("AfterRoundTrip: no span from before hook")
		return
	}
	ctx := data.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	defer metrics.AddActiveRequests(ctx, -1, data.activeMetricAttrs)

	span := data.span
	defer span.End()

	// Add response attributes
	if res != nil {
		attrs := httpsemconv.HTTPClientResponseTraceAttrs(res)
		span.SetAttributes(attrs...)

		// Set span status based on status code
		code, desc := httpsemconv.HTTPClientStatus(res.StatusCode)
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
		span.SetAttributes(httpsemconv.HTTPClientErrorType(err))
		logger.Debug("AfterRoundTrip called with error", "error", err)
	}

	if req := data.req; req != nil {
		statusCode := 0
		responseSize := int64(0)
		networkProtocol := req.Proto
		metricAttrs := []attribute.KeyValue(nil)
		if res != nil {
			statusCode = res.StatusCode
			responseSize = res.ContentLength
			networkProtocol = res.Proto
		}
		if err != nil {
			metricAttrs = append(metricAttrs, httpsemconv.HTTPClientErrorType(err))
		} else if res != nil {
			code, _ := httpsemconv.HTTPClientStatus(statusCode)
			if code == codes.Error {
				metricAttrs = append(
					metricAttrs,
					otelsemconv.ErrorTypeKey.String(strconv.Itoa(statusCode)),
				)
			}
		}
		metrics.RecordMetrics(
			ctx,
			req,
			statusCode,
			networkProtocol,
			req.ContentLength,
			responseSize,
			time.Since(data.start).Seconds(),
			metricAttrs,
		)
	}
}
