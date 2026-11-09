// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"github.com/labstack/echo/v5"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"

	httpsemconv "go.opentelemetry.io/otelc/instrumentation/net/http/semconv"
	"go.opentelemetry.io/otelc/pkg/hook"
)

const (
	routeSetKey    = "otel.echo.route.set"
	errRecordedKey = "otel.echo.err.recorded"
)

// AfterRoute runs after the router has populated the matched route in Context.
func AfterRoute(ictx hook.HookContext, _ echo.HandlerFunc) {
	if !enabler.Enable() || ictx == nil || ictx.GetParamCount() == 0 {
		return
	}

	c := firstEchoContext(ictx)
	if c == nil {
		return
	}
	req := c.Request()
	if req == nil {
		return
	}

	route := c.Path()
	if route == "" || c.Get(routeSetKey) != nil || c.Get(echo.ContextKeyHeaderAllow) != nil {
		return
	}

	span := trace.SpanFromContext(req.Context())
	if !span.IsRecording() {
		return
	}

	c.Set(routeSetKey, struct{}{})
	span.SetName(httpsemconv.HTTPServerSpanName(req.Method, route))
	span.SetAttributes(semconv.HTTPRoute(route))
	logger.Debug("echo route resolved", "route", route)
}

// AfterDefaultHTTPErrorHandler wraps the default error handler returned by Echo's factory.
func AfterDefaultHTTPErrorHandler(ictx hook.HookContext, handler echo.HTTPErrorHandler) {
	if !enabler.Enable() || ictx == nil || handler == nil {
		return
	}
	ictx.SetReturnVal(0, echo.HTTPErrorHandler(func(c *echo.Context, err error) {
		recordRequestError(c, err)
		handler(c, err)
	}))
}

func recordRequestError(c *echo.Context, err error) {
	if err == nil || c == nil || c.Request() == nil {
		return
	}
	if code := echo.StatusCode(err); code != 0 && code < 500 {
		return
	}
	if c.Get(errRecordedKey) != nil {
		return
	}
	span := trace.SpanFromContext(c.Request().Context())
	if !span.IsRecording() {
		return
	}
	c.Set(errRecordedKey, struct{}{})
	span.SetStatus(codes.Error, err.Error())
	span.RecordError(err)
}

func firstEchoContext(ictx hook.HookContext) *echo.Context {
	for i := 0; i < ictx.GetParamCount(); i++ {
		c, ok := ictx.GetParam(i).(*echo.Context)
		if ok && c != nil {
			return c
		}
	}
	return nil
}
