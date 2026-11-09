// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otelc/pkg/hook/hooktest"
)

func setupEchoV5Tracer(t *testing.T) (*tracetest.SpanRecorder, trace.Tracer) {
	t.Helper()
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "")

	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})
	return sr, tp.Tracer("echo-v5-test")
}

func TestAfterRoute_EnrichesMatchedRoute(t *testing.T) {
	sr, tracer := setupEchoV5Tracer(t)
	_, span := tracer.Start(context.Background(), "GET")

	e := echo.New()
	e.GET("/users/:id", func(*echo.Context) error { return nil })
	req := httptest.NewRequest(http.MethodGet, "/users/42", nil).
		WithContext(trace.ContextWithSpan(context.Background(), span))
	c := echo.NewContext(req, httptest.NewRecorder(), e)
	handler := e.Router().Route(c)

	AfterRoute(hooktest.NewMockHookContext(e.Router(), c), handler)
	span.End()

	require.Len(t, sr.Ended(), 1)
	ended := sr.Ended()[0]
	assert.Equal(t, "GET /users/:id", ended.Name())
	require.Len(t, ended.Attributes(), 1)
	assert.Equal(t, "http.route", string(ended.Attributes()[0].Key))
	assert.Equal(t, "/users/:id", ended.Attributes()[0].Value.AsString())
}

func TestAfterDefaultHTTPErrorHandler_RecordsServerErrors(t *testing.T) {
	sr, tracer := setupEchoV5Tracer(t)
	_, span := tracer.Start(context.Background(), "GET")
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/error", nil).
		WithContext(trace.ContextWithSpan(context.Background(), span))
	c := echo.NewContext(req, httptest.NewRecorder(), e)

	ictx := hooktest.NewMockHookContext()
	handler := echo.DefaultHTTPErrorHandler(false)
	AfterDefaultHTTPErrorHandler(ictx, handler)
	wrapped, ok := ictx.GetReturnVal(0).(echo.HTTPErrorHandler)
	require.True(t, ok)
	wrapped(c, errors.New("handler failed"))
	span.End()

	require.Len(t, sr.Ended(), 1)
	ended := sr.Ended()[0]
	assert.Equal(t, codes.Error, ended.Status().Code)
	assert.Contains(t, ended.Status().Description, "handler failed")
	require.Len(t, ended.Events(), 1)
}

func TestAfterDefaultHTTPErrorHandler_SkipsClientErrors(t *testing.T) {
	sr, tracer := setupEchoV5Tracer(t)
	_, span := tracer.Start(context.Background(), "GET")
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/missing", nil).
		WithContext(trace.ContextWithSpan(context.Background(), span))
	c := echo.NewContext(req, httptest.NewRecorder(), e)

	ictx := hooktest.NewMockHookContext()
	handler := echo.DefaultHTTPErrorHandler(false)
	AfterDefaultHTTPErrorHandler(ictx, handler)
	wrapped, ok := ictx.GetReturnVal(0).(echo.HTTPErrorHandler)
	require.True(t, ok)
	wrapped(c, echo.NewHTTPError(http.StatusNotFound, "missing"))
	span.End()

	require.Len(t, sr.Ended(), 1)
	assert.Equal(t, codes.Unset, sr.Ended()[0].Status().Code)
	assert.Empty(t, sr.Ended()[0].Events())
}
