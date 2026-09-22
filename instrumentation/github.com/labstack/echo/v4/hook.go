// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/http"
	"reflect"
	"sync"

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

// BeforeEchoServeHTTP wraps e.HTTPErrorHandler once so handler `return err`
// is recorded even when the application replaced the default handler.
func BeforeEchoServeHTTP(_ hook.HookContext, e *echo.Echo, _ http.ResponseWriter, _ *http.Request) {
	if !enabler.Enable() || e == nil {
		return
	}
	installErrorHandlerHook(e)
}

// BeforeDefaultHTTPErrorHandler runs before (*Echo).DefaultHTTPErrorHandler.
// ServeHTTP calls this for handler `return err` when the default handler is
// still installed.
func BeforeDefaultHTTPErrorHandler(_ hook.HookContext, _ *echo.Echo, err error, c echo.Context) {
	if !enabler.Enable() {
		return
	}
	recordRequestError(c, err)
}

// hookedErrorHandlers tracks Echo instances whose HTTPErrorHandler is wrapped.
var hookedErrorHandlers sync.Map

func installErrorHandlerHook(e *echo.Echo) {
	if _, loaded := hookedErrorHandlers.LoadOrStore(e, struct{}{}); loaded {
		return
	}
	orig := e.HTTPErrorHandler
	e.HTTPErrorHandler = func(err error, c echo.Context) {
		recordRequestError(c, err)
		if orig != nil {
			orig(err, c)
		}
	}
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
