// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst/insttest"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// newGinContextWithRoute creates a *gin.Context that has FullPath() populated
// by routing a real request through a minimal gin engine. The returned context
// has the given span embedded in c.Request.Context().
func newGinContextWithRoute(t *testing.T, method, routePattern, url string, span trace.Span) *gin.Context {
	t.Helper()

	var captured *gin.Context
	r := gin.New()
	r.Handle(method, routePattern, func(c *gin.Context) {
		captured = c
	})

	req := httptest.NewRequest(method, url, nil)
	if span != nil {
		ctx := trace.ContextWithSpan(context.Background(), span)
		req = req.WithContext(ctx)
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.NotNil(t, captured, "no handler was invoked; check route pattern and URL")
	return captured
}

func setupContextTracer(t *testing.T) (*tracetest.SpanRecorder, trace.Tracer) {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})
	return sr, tp.Tracer("test")
}

func TestBeforeNext_UpdatesSpanNameAndRoute(t *testing.T) {
	sr, tr := setupContextTracer(t)
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "GIN")

	_, span := tr.Start(context.Background(), "GET")
	c := newGinContextWithRoute(t, "GET", "/users/:id", "/users/42", span)

	ictx := insttest.NewMockHookContext(c)
	BeforeNext(ictx, c)

	span.End()
	require.Len(t, sr.Ended(), 1)
	ended := sr.Ended()[0]

	assert.Equal(t, "GET /users/:id", ended.Name(), "span name should include route pattern")

	attrs := make(map[string]interface{})
	for _, a := range ended.Attributes() {
		attrs[string(a.Key)] = a.Value.AsInterface()
	}
	assert.Equal(t, "/users/:id", attrs["http.route"], "http.route attribute should be the pattern, not the URL")
}

func TestBeforeNext_EmptyRouteIsNoop(t *testing.T) {
	sr, tr := setupContextTracer(t)
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "GIN")

	_, span := tr.Start(context.Background(), "GET")

	// gin.CreateTestContext produces a context with no router match,
	// so FullPath() returns "". BeforeNext should be a no-op.
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/does-not-exist", nil).
		WithContext(trace.ContextWithSpan(context.Background(), span))

	ictx := insttest.NewMockHookContext(c)
	BeforeNext(ictx, c)
	span.End()

	require.Len(t, sr.Ended(), 1)
	assert.Equal(t, "GET", sr.Ended()[0].Name(), "span name must not be modified when route is empty")
}

func TestBeforeNext_IdempotentOnMultipleNextCalls(t *testing.T) {
	sr, tr := setupContextTracer(t)
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "GIN")

	_, span := tr.Start(context.Background(), "GET")
	c := newGinContextWithRoute(t, "GET", "/items/:id", "/items/7", span)
	ictx := insttest.NewMockHookContext(c)

	// Simulate multiple middleware calling c.Next().
	BeforeNext(ictx, c)
	BeforeNext(ictx, c)
	BeforeNext(ictx, c)

	span.End()
	require.Len(t, sr.Ended(), 1)

	// http.route should appear exactly once (not duplicated by repeated calls).
	var routeAttrCount int
	for _, a := range sr.Ended()[0].Attributes() {
		if string(a.Key) == "http.route" {
			routeAttrCount++
		}
	}
	assert.Equal(
		t,
		1,
		routeAttrCount,
		"http.route should be set exactly once regardless of how many times Next is called",
	)
}

func TestBeforeNext_DisabledIsNoop(t *testing.T) {
	sr, tr := setupContextTracer(t)
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "GIN")

	_, span := tr.Start(context.Background(), "GET")
	c := newGinContextWithRoute(t, "GET", "/ping", "/ping", span)

	ictx := insttest.NewMockHookContext(c)
	BeforeNext(ictx, c)

	span.End()
	require.Len(t, sr.Ended(), 1)

	// Name should still be the initial "GET" since the hook is disabled.
	assert.Equal(t, "GET", sr.Ended()[0].Name())
}
