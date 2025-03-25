// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrumenter

import (
	"context"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"testing"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst-api/utils"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestNoopSpanSuppressor(t *testing.T) {
	ns := &NoneStrategy{}
	n := ns.create([]attribute.Key{})
	ctx := context.Background()
	n.StoreInContext(ctx, trace.SpanKindClient, noop.Span{})
	if n.ShouldSuppress(ctx, trace.SpanKindClient) != false {
		t.Errorf("should not suppress span")
	}
}

func TestSpanKeySuppressor(t *testing.T) {
	s := SpanKeySuppressor{
		spanKeys: []attribute.Key{
			utils.HTTP_CLIENT_KEY,
		},
	}
	builder := Builder[testRequest, testResponse]{}
	builder.Init().SetSpanNameExtractor(testNameExtractor{}).
		SetSpanKindExtractor(&AlwaysClientExtractor[testRequest]{}).
		SetInstrumentationScope(instrumentation.Scope{
			Name:      utils.FAST_HTTP_CLIENT_SCOPE_NAME,
			Version:   "test",
			SchemaURL: "test",
		})
	instrumenter := builder.BuildInstrumenter()
	ctx := context.Background()
	traceProvider := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(traceProvider)
	newCtx := instrumenter.Start(ctx, testRequest{})
	span := trace.SpanFromContext(newCtx)
	newCtx = s.StoreInContext(newCtx, trace.SpanKindClient, span)
	if !s.ShouldSuppress(newCtx, trace.SpanKindClient) {
		t.Errorf("should suppress span")
	}
}

func TestSpanKeySuppressorNotMatch(t *testing.T) {
	s := SpanKeySuppressor{
		spanKeys: []attribute.Key{
			utils.RPC_CLIENT_KEY,
		},
	}
	builder := Builder[testRequest, testResponse]{}
	builder.Init().SetSpanNameExtractor(testNameExtractor{}).
		SetSpanKindExtractor(&AlwaysClientExtractor[testRequest]{}).
		SetInstrumentationScope(instrumentation.Scope{
			Name:      utils.FAST_HTTP_CLIENT_SCOPE_NAME,
			Version:   "test",
			SchemaURL: "test",
		})
	instrumenter := builder.BuildInstrumenter()
	ctx := context.Background()
	traceProvider := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(traceProvider)
	newCtx := instrumenter.Start(ctx, testRequest{})
	span := trace.SpanFromContext(newCtx)
	newCtx = s.StoreInContext(newCtx, trace.SpanKindClient, span)
	if s.ShouldSuppress(newCtx, trace.SpanKindClient) {
		t.Errorf("should not suppress span with different span key")
	}
}

func TestSpanKindSuppressor(t *testing.T) {
	sks := &SpanKindStrategy{}
	s := sks.create([]attribute.Key{})
	builder := Builder[testRequest, testResponse]{}
	builder.Init().SetSpanNameExtractor(testNameExtractor{}).
		SetSpanKindExtractor(&AlwaysClientExtractor[testRequest]{}).
		SetInstrumentationScope(instrumentation.Scope{
			Name:      utils.FAST_HTTP_CLIENT_SCOPE_NAME,
			Version:   "test",
			SchemaURL: "test",
		})
	instrumenter := builder.BuildInstrumenter()
	ctx := context.Background()
	traceProvider := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(traceProvider)
	newCtx := instrumenter.Start(ctx, testRequest{})
	span := trace.SpanFromContext(newCtx)
	newCtx = s.StoreInContext(newCtx, trace.SpanKindClient, span)
	if !s.ShouldSuppress(newCtx, trace.SpanKindClient) {
		t.Errorf("should not suppress span with different span key")
	}
}
