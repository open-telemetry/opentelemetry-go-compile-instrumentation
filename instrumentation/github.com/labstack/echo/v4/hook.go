// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"errors"
	"reflect"

	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"

	httpsemconv "go.opentelemetry.io/otelc/instrumentation/net/http/semconv"
	"go.opentelemetry.io/otelc/pkg/hook"
)

// Keys stored on echo.Context. Reserved by this package.
const (
	routeSetKey    = "otel.echo.route.set"
	errRecordedKey = "otel.echo.err.recorded"
)

// AfterFind runs after (*Router).Find. The router has set Context.Path()
// to the matched template, or left it empty when nothing matched.
func AfterFind(ictx hook.HookContext) {
	if !enabler.Enable() {
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
	if route == "" || !matchedRoute(c) {
		return
	}
	if c.Get(routeSetKey) != nil {
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

// BeforeError runs before (*context).Error. The receiver is unexported, so
// the hook takes any and asserts to echo.Context.
func BeforeError(_ hook.HookContext, recv any, err error) {
	if !enabler.Enable() {
		return
	}
	c, _ := recv.(echo.Context)
	recordRequestError(c, err)
}

// BeforeDefaultHTTPErrorHandler runs before (*Echo).DefaultHTTPErrorHandler.
// ServeHTTP calls this for handler `return err`; c.Error() also lands here
// when the default handler is installed.
func BeforeDefaultHTTPErrorHandler(_ hook.HookContext, _ *echo.Echo, err error, c echo.Context) {
	if !enabler.Enable() {
		return
	}
	recordRequestError(c, err)
}

func recordRequestError(c echo.Context, err error) {
	if err == nil || c == nil || c.Request() == nil {
		return
	}
	var he *echo.HTTPError
	if errors.As(err, &he) && he.Code < 500 {
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

func firstEchoContext(ictx hook.HookContext) echo.Context {
	if ictx == nil {
		return nil
	}
	for i := 0; i < ictx.GetParamCount(); i++ {
		c, ok := ictx.GetParam(i).(echo.Context)
		if ok && c != nil {
			return c
		}
	}
	return nil
}

// matchedRoute is false for Echo's default 404/405 handlers. Find still
// writes Path() when a path node exists but the method does not (405),
// and stores allowed methods on ContextKeyHeaderAllow.
func matchedRoute(c echo.Context) bool {
	if c.Get(echo.ContextKeyHeaderAllow) != nil {
		return false
	}
	h := c.Handler()
	if h == nil {
		return false
	}
	// Go forbids func == func except vs nil.
	return reflect.ValueOf(h).Pointer() != reflect.ValueOf(echo.NotFoundHandler).Pointer()
}
