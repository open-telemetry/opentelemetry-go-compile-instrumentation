// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/http"
	"runtime/debug"
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
	instrumentationName = "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/nethttp"
	instrumentationKey  = "NETHTTP"
	responseWriterIndex = 1
	requestIndex        = 2
)

var (
	logger     = shared.Logger()
	tracer     trace.Tracer
	propagator propagation.TextMapPropagator
	initOnce   sync.Once
)

// moduleVersion extracts the version from the Go module system.
// Falls back to "dev" if version cannot be determined.
func moduleVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}

	// Return the main module version
	if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}

	return "dev"
}

func initInstrumentation() {
	initOnce.Do(func() {
		version := moduleVersion()
		if err := shared.SetupOTelSDK("go.opentelemetry.io/compile-instrumentation/nethttp/server", version); err != nil {
			logger.Error("failed to setup OTel SDK", "error", err)
		}
		tracer = otel.GetTracerProvider().Tracer(
			instrumentationName,
			trace.WithInstrumentationVersion(version),
		)
		propagator = otel.GetTextMapPropagator()

		// Start runtime metrics (respects OTEL_GO_ENABLED/DISABLED_INSTRUMENTATIONS)
		if err := shared.StartRuntimeMetrics(); err != nil {
			logger.Error("failed to start runtime metrics", "error", err)
		}

		logger.Info("HTTP server instrumentation initialized")
	})
}

// netHttpServerEnabler controls whether server instrumentation is enabled
type netHttpServerEnabler struct{}

func (n netHttpServerEnabler) Enable() bool {
	return shared.Instrumented(instrumentationKey)
}

var serverEnabler = netHttpServerEnabler{}

func BeforeServeHTTP(ictx inst.HookContext, recv interface{}, w http.ResponseWriter, r *http.Request) {
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
	attrs := semconv.HTTPServerRequestTraceAttrs("", r)

	// Get HTTP route from r.Pattern (Go 1.22+)
	route := semconv.HTTPRoute(r.Pattern)
	spanName := semconv.HTTPServerSpanName(r.Method, route)

	// Start span
	ctx, span := tracer.Start(ctx,
		spanName,
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(attrs...),
	)

	// Add route attribute if available
	if route != "" {
		span.SetAttributes(semconv.HTTPServerRoute(route))
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

	// Store data for after hook
	ictx.SetData(map[string]interface{}{
		"ctx":   ctx,
		"span":  span,
		"start": time.Now(),
	})
}

func AfterServeHTTP(ictx inst.HookContext) {
	if !serverEnabler.Enable() {
		return
	}

	span, ok := ictx.GetKeyData("span").(trace.Span)
	if !ok || span == nil {
		logger.Debug("AfterServeHTTP: no span from before hook")
		return
	}
	defer span.End()

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

	startTime, _ := ictx.GetKeyData("start").(time.Time)
	logger.Debug("AfterServeHTTP called",
		"status_code", statusCode,
		"duration_ms", time.Since(startTime).Milliseconds())

	logger.Debug("AfterServeHTTP completed")
}
