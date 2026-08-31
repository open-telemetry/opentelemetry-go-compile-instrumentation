// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package v82 provides compile-time OpenTelemetry instrumentation for
// github.com/stripe/stripe-go/v82.
//
// # Why a single hook
//
// stripe-go exposes over 200 per-resource clients (customer.Client,
// paymentintent.Client, …) with thousands of exported methods, but every one of
// them ultimately reaches the network through a single unexported chokepoint on
// the backend:
//
//	Call ─┐
//	CallRaw ────────┐
//	CallMultipart ──┤
//	CallStreaming ──┼─► (*BackendImplementation).requestWithRetriesAndTelemetry
//	RawRequest ─────┘
//
// Hooking that one function covers the entire SDK surface: v1 and v2 API modes,
// the deprecated client.API aggregate, and the current stripe.Client. It needs
// no hook generated per method, and does not break when Stripe adds a
// resource.
//
// # Span shape
//
// One CLIENT span per logical API request, named "{METHOD} {url.template}"
// (e.g. "POST /v1/customers/{id}"). The span spans the backend's internal retry
// loop, so a request retried three times is one span, not three. When net/http
// client instrumentation is also enabled, each individual attempt appears as a
// child RoundTrip span carrying its own timing.
//
// # Layering
//
// This file owns the hook lifecycle and is the only place that imports
// stripe-go. Attribute and span-name policy lives in the semconv subpackage,
// which depends only on the standard library and OpenTelemetry; stripe error
// types are converted to semconv.APIError at the boundary. A future stripe-go
// major version therefore only needs this file re-pointed, not the conventions
// re-derived.
package v82

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	stripe "github.com/stripe/stripe-go/v82"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otelc/instrumentation/github.com/stripe/stripe-go/v82/semconv"
	"go.opentelemetry.io/otelc/pkg/hook"
	"go.opentelemetry.io/otelc/pkg/runtime"
)

const (
	instrumentationName = "go.opentelemetry.io/otelc/instrumentation/github.com/stripe/stripe-go/v82"
	instrumentationKey  = "STRIPE"

	// reqParamIndex is the *http.Request parameter of
	// requestWithRetriesAndTelemetry; the receiver occupies index 0.
	reqParamIndex = 1

	keySpan    = "span"
	keyStart   = "start"
	keyRequest = "request"
	keyCtx     = "ctx"
)

var (
	logger   = runtime.Logger()
	tracer   trace.Tracer
	metrics  semconv.Metrics
	initOnce sync.Once
)

// stripeEnabler controls whether this instrumentation is enabled at runtime via
// OTEL_GO_ENABLED_INSTRUMENTATIONS / OTEL_GO_DISABLED_INSTRUMENTATIONS.
type stripeEnabler struct{}

func (stripeEnabler) Enable() bool { return runtime.Instrumented(instrumentationKey) }

var enabler = stripeEnabler{}

func initInstrumentation() {
	initOnce.Do(func() {
		version := runtime.ModuleVersion()
		tracer = otel.GetTracerProvider().Tracer(
			instrumentationName,
			trace.WithInstrumentationVersion(version),
		)
		meter := otel.GetMeterProvider().Meter(
			instrumentationName,
			metric.WithInstrumentationVersion(version),
		)
		metrics = semconv.NewMetrics(meter)
		logger.Info("stripe instrumentation initialized")
	})
}

// BeforeRequest starts the span for a logical Stripe API request.
//
// The handleResponse callback parameter is accepted as interface{} because its
// func type carries no information the hook needs.
func BeforeRequest(
	ictx hook.HookContext,
	_ *stripe.BackendImplementation,
	req *http.Request,
	_ *bytes.Buffer,
	_ interface{}, // handleResponse callback
) {
	if !enabler.Enable() {
		logger.Debug("stripe instrumentation disabled")
		return
	}
	if req == nil || req.URL == nil {
		return
	}
	initInstrumentation()

	sreq := semconv.Request{
		Method:        req.Method,
		Path:          req.URL.Path,
		ServerAddress: req.URL.Hostname(),
		ServerPort:    serverPort(req.URL),
	}

	ctx, span := tracer.Start(req.Context(),
		semconv.SpanName(sreq.Method, sreq.Path),
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.RequestTraceAttrs(sreq)...),
	)

	// Re-parent the outgoing request so each retry attempt instrumented by
	// net/http becomes a child of this span rather than a trace root. The copy
	// shares the original's header map, so headers set later inside the retry
	// loop still apply.
	ictx.SetParam(reqParamIndex, req.WithContext(ctx))

	ictx.SetKeyData(keySpan, span)
	ictx.SetKeyData(keyStart, time.Now())
	ictx.SetKeyData(keyRequest, sreq)
	ictx.SetKeyData(keyCtx, ctx)

	logger.Debug("BeforeRequest", "method", sreq.Method, "path", sreq.Path)
}

// AfterRequest ends the span started by BeforeRequest.
//
// The backend returns a nil response alongside a non-nil error, so the status
// code on the failure path comes from the Stripe error rather than the response.
func AfterRequest(
	ictx hook.HookContext,
	resp *http.Response,
	_ interface{}, // handleResponse result
	_ *time.Duration, // last-attempt duration; the span covers all attempts
	err error,
) {
	span, ok := ictx.GetKeyData(keySpan).(trace.Span)
	if !ok || span == nil {
		// BeforeRequest was disabled or bailed out; nothing to close.
		return
	}
	defer span.End()

	sreq, _ := ictx.GetKeyData(keyRequest).(semconv.Request)
	start, _ := ictx.GetKeyData(keyStart).(time.Time)
	ctx, _ := ictx.GetKeyData(keyCtx).(context.Context)
	if ctx == nil {
		ctx = context.Background()
	}

	statusCode := 0
	switch {
	case err != nil:
		apiErr := apiErrorFrom(err)
		statusCode = apiErr.StatusCode
		span.RecordError(err)
		span.SetAttributes(semconv.ErrorTraceAttrs(err, apiErr)...)
		if statusCode > 0 {
			if code, desc := semconv.HTTPClientStatus(statusCode); code != codes.Unset {
				span.SetStatus(code, desc)
			}
		} else {
			span.SetStatus(codes.Error, err.Error())
		}
		logger.Debug("AfterRequest error", "error", err)
	case resp != nil:
		statusCode = resp.StatusCode
		span.SetAttributes(semconv.ResponseTraceAttrs(statusCode, resp.Header.Get("Request-Id"))...)
		if code, desc := semconv.HTTPClientStatus(statusCode); code != codes.Unset {
			span.SetStatus(code, desc)
		}
	}

	if !start.IsZero() {
		metrics.RecordRequestDuration(ctx, time.Since(start).Seconds(), sreq, statusCode)
	}

	logger.Debug("AfterRequest completed", "status", statusCode)
}

// serverPort resolves the request's port, falling back to the scheme default so
// server.port is reported even when the URL leaves it implicit.
func serverPort(u *url.URL) int {
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			return n
		}
	}
	switch u.Scheme {
	case "https":
		return 443
	case "http":
		return 80
	}
	return 0
}

// apiErrorFrom converts a stripe-go error into the version-neutral form the
// semconv package consumes. Transport failures (no Stripe response at all)
// yield the zero value, and semconv.ErrorType falls back to the Go error type.
func apiErrorFrom(err error) semconv.APIError {
	var v1 *stripe.Error
	if errors.As(err, &v1) && v1 != nil {
		return semconv.APIError{
			StatusCode: v1.HTTPStatusCode,
			Code:       string(v1.Code),
			Type:       string(v1.Type),
			RequestID:  v1.RequestID,
		}
	}
	// v2 API errors carry a code and type but no status code or request id.
	var v2 *stripe.V2RawError
	if errors.As(err, &v2) && v2 != nil {
		e := semconv.APIError{Code: v2.Code}
		if v2.Type != nil {
			e.Type = string(*v2.Type)
		}
		return e
	}
	return semconv.APIError{}
}
