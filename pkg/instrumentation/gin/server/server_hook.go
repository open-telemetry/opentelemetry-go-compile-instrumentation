// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/http"
	"runtime/debug"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
	httpsemconv "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/nethttp/semconv"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/shared"
)

const (
	instrumentationName = "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/gin"
	instrumentationKey  = "GIN"
	responseWriterIndex = 1
	requestIndex        = 2
)

var (
	logger     = shared.Logger()
	tracer     trace.Tracer
	propagator propagation.TextMapPropagator
	initOnce   sync.Once
)

func moduleVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

func initInstrumentation() {
	initOnce.Do(func() {
		version := moduleVersion()
		if err := shared.SetupOTelSDK(
			"go.opentelemetry.io/compile-instrumentation/gin/server",
			version,
		); err != nil {
			logger.Error("failed to setup OTel SDK", "error", err)
		}
		tracer = otel.GetTracerProvider().Tracer(
			instrumentationName,
			trace.WithInstrumentationVersion(version),
		)
		propagator = otel.GetTextMapPropagator()

		if err := shared.StartRuntimeMetrics(); err != nil {
			logger.Error("failed to start runtime metrics", "error", err)
		}

		logger.Info("Gin server instrumentation initialized")
	})
}

type ginServerEnabler struct{}

func (g ginServerEnabler) Enable() bool {
	return shared.Instrumented(instrumentationKey)
}

var serverEnabler = ginServerEnabler{}

// writerWrapper captures the HTTP status code written through to the response.
// Gin's internal responseWriter calls through to the underlying http.ResponseWriter
// so wrapping it here lets us observe the final status code.
type writerWrapper struct {
	http.ResponseWriter
	statusCode int
}

func (w *writerWrapper) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *writerWrapper) Write(b []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// BeforeServeHTTP hooks before (*gin.Engine).ServeHTTP to start an OTel span.
//
// Hook parameter mapping for (*gin.Engine).ServeHTTP(w http.ResponseWriter, req *http.Request):
//
//	recv  = 0  (*gin.Engine)
//	w     = 1  (http.ResponseWriter)
//	req   = 2  (*http.Request)
func BeforeServeHTTP(ictx inst.HookContext, recv *gin.Engine, w http.ResponseWriter, req *http.Request) {
	if !serverEnabler.Enable() {
		logger.Debug("Gin server instrumentation disabled")
		return
	}

	initInstrumentation()

	logger.Debug("BeforeServeHTTP called",
		"method", req.Method,
		"url", req.URL.String(),
		"remote_addr", req.RemoteAddr)

	// Extract incoming trace context from request headers.
	ctx := propagator.Extract(req.Context(), propagation.HeaderCarrier(req.Header))

	attrs := httpsemconv.HTTPServerRequestTraceAttrs("", req)
	spanName := httpsemconv.HTTPServerSpanName(req.Method, "")

	ctx, span := tracer.Start(ctx,
		spanName,
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(attrs...),
	)

	// Wrap the ResponseWriter so we can capture the status code written by the handler.
	// Gin calls through to the underlying http.ResponseWriter, so this wrapper intercepts it.
	wrapper := &writerWrapper{ResponseWriter: w, statusCode: 0}
	ictx.SetParam(responseWriterIndex, wrapper)

	// Propagate the span context into the request.
	newReq := req.WithContext(ctx)
	ictx.SetParam(requestIndex, newReq)

	ictx.SetData(map[string]interface{}{
		"ctx":   ctx,
		"span":  span,
		"start": time.Now(),
	})
}

// AfterServeHTTP hooks after (*gin.Engine).ServeHTTP to end the OTel span.
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

	statusCode := http.StatusOK
	if wrapper, ok := ictx.GetParam(responseWriterIndex).(*writerWrapper); ok && wrapper.statusCode != 0 {
		statusCode = wrapper.statusCode
	}

	attrs := httpsemconv.HTTPServerResponseTraceAttrs(statusCode, 0)
	span.SetAttributes(attrs...)

	code, desc := httpsemconv.HTTPServerStatus(statusCode)
	if code != codes.Unset {
		span.SetStatus(code, desc)
	}

	startTime, _ := ictx.GetKeyData("start").(time.Time)
	logger.Debug("AfterServeHTTP completed",
		"status_code", statusCode,
		"duration_ms", time.Since(startTime).Milliseconds())
}
