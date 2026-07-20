// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package v2 provides compile-time OpenTelemetry instrumentation for
// github.com/linode/linodego/v2.
//
// Two span layers are produced:
//
//  1. Public *Client API methods (GetInstance, ListVolumes, …) — CLIENT spans
//     named linodego.<Method>, covering the full operation (including
//     multi-page list aggregation).
//  2. (*Client).doRequest — CLIENT spans named "{METHOD} {endpoint}" for each
//     underlying HTTP call (retries and pagination pages).
//
// Metrics:
//
//   - linodego.client.operation.duration — public API method latency, labeled by
//     operation name and status code (bounded). Raw paths with resource IDs are
//     not used as metric labels.
//
// When net/http client instrumentation is also enabled, RoundTrip spans nest
// under the doRequest span via context propagation.
package v2

import (
	"context"
	"sync"

	"github.com/linode/linodego/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otelc/instrumentation/github.com/linode/linodego/v2/semconv"
	"go.opentelemetry.io/otelc/pkg/hook"
	"go.opentelemetry.io/otelc/pkg/runtime"
)

const (
	instrumentationName = "go.opentelemetry.io/otelc/instrumentation/github.com/linode/linodego/v2"
	instrumentationKey  = "LINODEGO"

	// doRequest param indices (receiver is 0).
	ctxParamIndex = 1

	keySpan      = "span"
	keyStart     = "start"
	keyOperation = "operation"
	keyCtx       = "ctx"
)

var (
	logger   = runtime.Logger()
	tracer   trace.Tracer
	metrics  semconv.Metrics
	initOnce sync.Once
)

// linodegoEnabler controls whether library instrumentation is enabled.
type linodegoEnabler struct{}

func (linodegoEnabler) Enable() bool {
	return runtime.Instrumented(instrumentationKey)
}

var enabler = linodegoEnabler{}

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
		logger.Info("linodego instrumentation initialized")
	})
}

// BeforeDoRequest runs before (*Client).doRequest.
//
// Unexported parameter types (requestParams, pagination mutator) are accepted
// as interface{} per the instrument guide.
func BeforeDoRequest(
	ictx hook.HookContext,
	_ *linodego.Client,
	ctx context.Context,
	method, endpoint string,
	_ interface{}, // requestParams
	_ interface{}, // paginationMutator
) {
	if !enabler.Enable() {
		logger.Debug("linodego instrumentation disabled")
		return
	}
	initInstrumentation()

	req := semconv.LinodegoRequest{
		Method:   method,
		Endpoint: endpoint,
	}
	attrs := semconv.LinodegoRequestTraceAttrs(req)
	spanName := semconv.SpanName(method, endpoint)

	ctx, span := tracer.Start(ctx,
		spanName,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrs...),
	)

	// Propagate the span context into the original request so nested
	// instrumentations (e.g. net/http) and public-method parents link correctly.
	ictx.SetParam(ctxParamIndex, ctx)
	ictx.SetKeyData(keySpan, span)

	logger.Debug("BeforeDoRequest", "method", method, "endpoint", endpoint)
}

// AfterDoRequest runs after (*Client).doRequest and ends the span.
func AfterDoRequest(ictx hook.HookContext, err error) {
	if !enabler.Enable() {
		return
	}

	span, ok := ictx.GetKeyData(keySpan).(trace.Span)
	if !ok || span == nil {
		logger.Debug("AfterDoRequest: no span from before hook")
		return
	}
	defer span.End()

	if err != nil {
		span.RecordError(err)
		if code, ok := semconv.StatusCodeFromError(err); ok {
			span.SetAttributes(semconv.LinodegoErrorTraceAttrs(err)...)
			if sc, desc := semconv.HTTPClientStatus(code); sc != codes.Unset {
				span.SetStatus(sc, desc)
			}
		} else {
			span.SetStatus(codes.Error, err.Error())
		}
		logger.Debug("AfterDoRequest error", "error", err)
	}

	logger.Debug("AfterDoRequest completed")
}

// finishSpanWithError records error details on a span (shared by public methods).
func finishSpanWithError(span trace.Span, err error) int {
	if err == nil {
		return 0
	}
	span.RecordError(err)
	if code, ok := semconv.StatusCodeFromError(err); ok {
		span.SetAttributes(semconv.LinodegoErrorTraceAttrs(err)...)
		if sc, desc := semconv.HTTPClientStatus(code); sc != codes.Unset {
			span.SetStatus(sc, desc)
		}
		return code
	}
	span.SetStatus(codes.Error, err.Error())
	return 0
}
