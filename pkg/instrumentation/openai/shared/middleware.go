// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package shared

import (
	"net/http"
	"time"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/openai/semconv"
)

// MiddlewareNext matches option.MiddlewareNext from every openai-go major
// version. Because option.MiddlewareNext is a type alias to this exact
// signature, OtelMiddleware can be passed to option.WithMiddleware in v1, v2,
// and v3 without any conversion.
type MiddlewareNext = func(*http.Request) (*http.Response, error)

// OtelMiddleware is an HTTP middleware that creates an OpenTelemetry GenAI
// span around every OpenAI API call, records duration and token-usage
// metrics, and sets error status on failures. It is version-independent: it
// talks only to the standard net/http types, not to any openai-go type.
//
// It is wired into each SDK version by a thin hook that calls
// option.WithMiddleware(shared.OtelMiddleware) on client construction.
func OtelMiddleware(req *http.Request, next MiddlewareNext) (*http.Response, error) {
	if !Enabled() {
		return next(req)
	}
	initInstrumentation()

	operation := parseRoute(req.URL.Path)

	// Buffer the request body so we can read the model out of it, then
	// restore it for the SDK's real roundtrip. Parse failures are
	// non-fatal; we still create the span.
	reqBuf, reqParsed := bufferAndParseRequest(req)
	if reqBuf != nil {
		req.Body = restoreBody(reqBuf)
	}

	var (
		model      string
		isStream   bool
	)
	if reqParsed != nil {
		model = reqParsed.Model
		isStream = reqParsed.Stream
	}

	spanName := operation
	if model != "" {
		spanName = operation + " " + model
	}
	ctx, span := tracer.Start(req.Context(), spanName,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.RequestTraceAttrs(operation, model)...),
	)
	req = req.WithContext(ctx)

	start := time.Now()
	resp, err := next(req)
	durationSec := time.Since(start).Seconds()

	recordDuration(ctx, operation, model, durationSec, err)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.End()
		return resp, err
	}

	if resp != nil {
		// Mark HTTP error statuses on the span but still try to parse the
		// body since the SDK will return the error payload to the caller.
		if resp.StatusCode >= http.StatusBadRequest {
			span.SetStatus(codes.Error, resp.Status)
		}

		// Streaming responses (SSE) must not be buffered: the SDK relies on
		// the live stream, and buffering would both break iteration and
		// consume unbounded memory. Token usage for streams is a TODO that
		// needs event-stream parsing.
		if !isStream && !isStreamingResponse(resp) {
			respBuf, respParsed := bufferAndParseResponse(resp)
			if respBuf != nil {
				resp.Body = restoreBody(respBuf)
			}
			if respParsed != nil {
				span.SetAttributes(semconv.ChatCompletionResponseTraceAttrs(
					respParsed.ID,
					respParsed.Model,
					respParsed.finishReasons(),
					respParsed.Usage.PromptTokens,
					respParsed.Usage.CompletionTokens,
				)...)
				recordTokenUsage(ctx, operation, model,
					respParsed.Usage.PromptTokens,
					respParsed.Usage.CompletionTokens)
			}
		}
	}

	span.End()
	return resp, err
}
