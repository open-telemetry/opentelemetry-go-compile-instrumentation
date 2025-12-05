// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/nethttp/semconv"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/shared"
)

const (
	otelExporterPrefix     = "OTel OTLP Exporter Go"
	instrumentationName    = "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/nethttp"
	instrumentationVersion = "0.1.0"
	instrumentationKey     = "NETHTTP"
	requestParamIndex      = 1
)

var (
	logger     = shared.GetLogger()
	tracer     trace.Tracer
	propagator propagation.TextMapPropagator
	initOnce   sync.Once
)

func initInstrumentation() {
	initOnce.Do(func() {
		if err := shared.SetupOTelSDK(); err != nil {
			logger.Error("failed to setup OTel SDK", "error", err)
		}
		tracer = otel.GetTracerProvider().Tracer(
			instrumentationName,
			trace.WithInstrumentationVersion(instrumentationVersion),
		)
		propagator = otel.GetTextMapPropagator()
		logger.Info("HTTP client instrumentation initialized")
	})
}

// netHttpClientEnabler controls whether client instrumentation is enabled
type netHttpClientEnabler struct{}

func (n netHttpClientEnabler) Enable() bool {
	return shared.Instrumented(instrumentationKey)
}

var clientEnabler = netHttpClientEnabler{}

func BeforeRoundTrip(ictx inst.HookContext, transport *http.Transport, req *http.Request) {
	if !clientEnabler.Enable() {
		logger.Debug("HTTP client instrumentation disabled")
		return
	}

	// Filter out OTel exporter requests to prevent infinite loops
	if strings.HasPrefix(req.Header.Get("User-Agent"), otelExporterPrefix) {
		logger.Debug("Skipping OTel exporter request", "user_agent", req.Header.Get("User-Agent"))
		return
	}

	initInstrumentation()

	logger.Debug("BeforeRoundTrip called",
		"method", req.Method,
		"url", req.URL.String(),
		"host", req.Host)

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
	ictx.SetData(map[string]interface{}{
		"ctx":   ctx,
		"span":  span,
		"req":   req,
		"start": time.Now(),
	})
}

func AfterRoundTrip(ictx inst.HookContext, res *http.Response, err error) {
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
		startTime, _ := ictx.GetKeyData("start").(time.Time)
		attrs := semconv.HTTPClientResponseTraceAttrs(res)
		span.SetAttributes(attrs...)

		// Set span status based on status code
		code, desc := semconv.HTTPClientStatus(res.StatusCode)
		if code != codes.Unset {
			span.SetStatus(code, desc)
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
		span.SetAttributes(semconv.HTTPClientErrorType(err))
		logger.Debug("AfterRoundTrip called with error", "error", err)
	}

	logger.Debug("AfterRoundTrip completed")
}
