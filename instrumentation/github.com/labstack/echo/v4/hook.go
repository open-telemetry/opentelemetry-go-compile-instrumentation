// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
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
	routeSetKey       = "otel.echo.route.set"
	errRecordedKey    = "otel.echo.err.recorded"
	handlerWrappedKey = "otel.echo.handler.wrapped"
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

	// matchedRoute compares c.Handler() with Echo's 404/405 handlers, so it
	// must run before wrapHandler replaces it.
	route := c.Path()
	matched := route != "" && matchedRoute(c)
	wrapHandler(c)
	if !matched || c.Get(routeSetKey) != nil {
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
// It covers errors returned by global middleware, which run outside the
// handler that wrapHandler wraps.
func BeforeDefaultHTTPErrorHandler(_ hook.HookContext, _ *echo.Echo, err error, c echo.Context) {
	if !enabler.Enable() {
		return
	}
	recordRequestError(c, err)
}

// wrapHandler records the error returned by the request's handler, so
// `return err` is recorded even with a custom e.HTTPErrorHandler. It changes
// only this request's Context, never the shared *echo.Echo.
func wrapHandler(c echo.Context) {
	if c.Get(handlerWrappedKey) != nil {
		return
	}
	next := c.Handler()
	if next == nil {
		return
	}
	c.Set(handlerWrappedKey, struct{}{})
	c.SetHandler(func(c echo.Context) error {
		err := next(c)
		if err != nil {
			recordRequestError(c, err)
		}
		return err
	})
}

func recordRequestError(c echo.Context, err error) {
	if err == nil || c == nil || c.Request() == nil {
		return
	}
	if he, ok := effectiveHTTPError(err); ok && he.Code < 500 {
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

// effectiveHTTPError follows DefaultHTTPErrorHandler: one Internal unwrap
// when that value is also *echo.HTTPError.
func effectiveHTTPError(err error) (*echo.HTTPError, bool) {
	he, ok := err.(*echo.HTTPError)
	if !ok {
		return nil, false
	}
	if herr, ok := he.Internal.(*echo.HTTPError); ok {
		he = herr
	}
	return he, true
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
// writes Path() when a path node exists but the method does not (405).
// Compare handler pointers; ContextKeyHeaderAllow is not in early v4.
func matchedRoute(c echo.Context) bool {
	h := c.Handler()
	if h == nil {
		return false
	}
	// Go forbids func == func except vs nil.
	hp := reflect.ValueOf(h).Pointer()
	if hp == reflect.ValueOf(echo.NotFoundHandler).Pointer() {
		return false
	}
	return hp != reflect.ValueOf(echo.MethodNotAllowedHandler).Pointer()
}
