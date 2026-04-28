// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package basic

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	instrumentationName    = "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/helloworld"
	instrumentationVersion = "0.1.0"
)

// HelloWorldRequest represents a simple request for demonstration purposes
type HelloWorldRequest struct {
	Path   string
	Params map[string]string
}

// HelloWorldResponse represents a simple response for demonstration purposes
type HelloWorldResponse struct {
	Status int
}

var tracer trace.Tracer

func init() {
	tracer = otel.GetTracerProvider().Tracer(
		instrumentationName,
		trace.WithInstrumentationVersion(instrumentationVersion),
	)
}

// StartInstrumentation starts a span for the hello world operation
func StartInstrumentation(ctx context.Context, req HelloWorldRequest) (context.Context, trace.Span) {
	// Create attributes from request
	attrs := []attribute.KeyValue{
		attribute.String("hello.path", req.Path),
	}

	// Add params as attributes if present
	for key, value := range req.Params {
		attrs = append(attrs, attribute.String("hello.param."+key, value))
	}

	// Start span with attributes
	ctx, span := tracer.Start(ctx,
		"hello-world",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attrs...),
	)

	return ctx, span
}

// EndInstrumentation ends the span and records response attributes
func EndInstrumentation(span trace.Span, resp HelloWorldResponse) {
	// Add response attributes
	span.SetAttributes(
		attribute.Int("hello.status", resp.Status),
	)

	span.End()
}
