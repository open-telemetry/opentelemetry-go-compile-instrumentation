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

	route := c.Path()

	// Capture handler identity before wrapping: Echo's default 404/405
	// handlers must not count as a matched route.
	matched := matchedRoute(c)

	// Wrap the per-request handler so a handler `return err` is recorded on
	// the net/http server span before ServeHTTP dispatches to
	// e.HTTPErrorHandler, whatever error handler the application installed.
	// ServeHTTP reads c.Handler() after (*Router).Find returns, so the wrap is
	// picked up by the request that is being routed. Mutating the
	// per-request echo.Context is race-free, unlike writing
	// e.HTTPErrorHandler, which Echo's ServeHTTP reads without a lock.
	// Wrapped before the route checks so unmatched requests (custom 404
	// handlers) are covered too; recordRequestError still skips 4xx.
	wrapHandler(c)

	if route == "" || !matched {
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
// the hook takes any and asserts to echo.Context. This covers handlers that
// call c.Error(err) and return nil: the wrapped handler sees a nil error, but
// c.Error invokes e.HTTPErrorHandler directly and this hook fires first.
func BeforeError(_ hook.HookContext, recv any, err error) {
	if !enabler.Enable() {
		return
	}
	c, _ := recv.(echo.Context)
	recordRequestError(c, err)
}

// BeforeDefaultHTTPErrorHandler runs before (*Echo).DefaultHTTPErrorHandler.
// ServeHTTP calls this for handler `return err` when the default handler is
// still installed. It is idempotent with the wrapped handler in AfterFind
// (recordRequestError gates on errRecordedKey).
func BeforeDefaultHTTPErrorHandler(_ hook.HookContext, _ *echo.Echo, err error, c echo.Context) {
	if !enabler.Enable() {
		return
	}
	recordRequestError(c, err)
}

// wrapHandler wraps c.Handler() so the error a handler returns is recorded
// before ServeHTTP dispatches to e.HTTPErrorHandler. This reaches custom
// error handlers, which replace DefaultHTTPErrorHandler and therefore
// bypass its hook. ServeHTTP applies its middleware chain around the wrapped
// handler, so middleware that swallows or rewrites the error runs after the
// original error has been recorded.
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
