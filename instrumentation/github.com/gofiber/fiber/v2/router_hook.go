// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otelc/pkg/hook"
)

// depthLocalKey counts how deep we are inside (*App).next for one request.
//
// Fiber re-enters the router rather than looping: a middleware that calls
// c.Next() runs (*Ctx).Next, which calls (*App).next again once the current
// route's handler stack is exhausted. Only the outermost frame corresponds to
// the request as a whole, so the span is opened and closed there. The counter
// lives in Locals because it has to be shared across those frames, whereas each
// frame gets its own HookContext.
const depthLocalKey = "otelc.fiber.next.depth"

const (
	// enabledDataKey and spanDataKey are per-frame, so they go in the
	// HookContext rather than in Locals.
	enabledDataKey   = "otelc.fiber.enabled"
	spanDataKey      = "otelc.fiber.span"
	outermostDataKey = "otelc.fiber.outermost"
)

// headerCarrier adapts the incoming Fiber request headers to the propagation
// API. Fiber is built on fasthttp, not net/http, so propagation.HeaderCarrier
// is not usable here. Going through *fiber.Ctx rather than the underlying
// *fasthttp.RequestHeader keeps fasthttp out of this module's direct
// dependencies: naming the type would require the import, calling methods on it
// does not.
type headerCarrier struct {
	c *fiber.Ctx
}

func (h headerCarrier) Get(key string) string { return h.c.Get(key) }

func (h headerCarrier) Set(key, value string) { h.c.Request().Header.Set(key, value) }

func (h headerCarrier) Keys() []string {
	keys := make([]string, 0, h.c.Request().Header.Len())
	h.c.Request().Header.VisitAll(func(key, _ []byte) {
		keys = append(keys, string(key))
	})
	return keys
}

// BeforeNext runs before (*App).next, Fiber's router entry point.
//
// (*App).handler calls it exactly once per request, before any handler or
// middleware runs, which is why the server span is created here rather than in
// (*Ctx).Next: a route with a single handler and no middleware never calls
// c.Next(), so a hook on that method sees no traffic at all.
func BeforeNext(ictx hook.HookContext, _ *fiber.App, c *fiber.Ctx) {
	enabled := enabler.Enable()
	ictx.SetKeyData(enabledDataKey, enabled)
	ictx.SetKeyData(outermostDataKey, false)
	if !enabled || c == nil {
		return
	}

	depth, _ := c.Locals(depthLocalKey).(int)
	c.Locals(depthLocalKey, depth+1)
	if depth > 0 {
		// Re-entered through (*Ctx).Next; the outermost frame owns the span.
		return
	}
	ictx.SetKeyData(outermostDataKey, true)

	initInstrumentation()

	ctx := propagator.Extract(c.UserContext(), headerCarrier{c: c})

	// The route pattern is not known yet: (*App).next assigns c.route only once
	// it has found a match. The span is named for the method alone here and
	// renamed in AfterNext, which is the same low-cardinality
	// "{method} {route}" shape the HTTP conventions ask for.
	method := c.Method()
	ctx, span := tracer.Start(ctx, method,
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(requestAttrs(c)...),
	)

	// Hand the span down so anything the handler calls nests under it.
	c.SetUserContext(ctx)

	ictx.SetKeyData(spanDataKey, span)
}

// AfterNext runs after (*App).next returns. matched reports whether any route
// matched; nextErr is the error a handler or the router produced.
func AfterNext(ictx hook.HookContext, _ bool, nextErr error) {
	if enabled, _ := ictx.GetKeyData(enabledDataKey).(bool); !enabled {
		return
	}

	// GetParam(0) is the *fiber.App receiver, GetParam(1) the *fiber.Ctx.
	c, ok := ictx.GetParam(1).(*fiber.Ctx)
	if !ok || c == nil {
		return
	}

	if depth, depthOK := c.Locals(depthLocalKey).(int); depthOK && depth > 0 {
		c.Locals(depthLocalKey, depth-1)
	}

	if outermost, _ := ictx.GetKeyData(outermostDataKey).(bool); !outermost {
		return
	}

	span, ok := ictx.GetKeyData(spanDataKey).(trace.Span)
	if !ok || span == nil {
		return
	}
	defer span.End()

	if route := c.Route(); route != nil && route.Path != "" {
		span.SetName(c.Method() + " " + route.Path)
		span.SetAttributes(semconv.HTTPRouteKey.String(route.Path))
	}

	status := responseStatus(c, nextErr)
	span.SetAttributes(semconv.HTTPResponseStatusCodeKey.Int(status))

	if nextErr != nil {
		span.RecordError(nextErr)
	}

	// Only 5xx marks a server span as failed. A 4xx is the client's problem and
	// the HTTP conventions leave such spans Unset.
	if status >= fiber.StatusInternalServerError {
		span.SetStatus(codes.Error, statusMessage(nextErr))
	}
}

// responseStatus reports the status the client will actually receive.
//
// (*App).next returns its error to (*App).handler, which only then runs the
// application's error handler and writes a status. This hook is inside that
// window, so the recorded response still holds whatever was set before the
// error, usually 200. Deriving the status from the error instead keeps 404,
// 405 and handler failures from being reported as successes.
func responseStatus(c *fiber.Ctx, nextErr error) int {
	if nextErr == nil {
		return c.Response().StatusCode()
	}
	var fiberErr *fiber.Error
	if errors.As(nextErr, &fiberErr) {
		return fiberErr.Code
	}
	// Fiber's default error handler turns anything else into a 500.
	return fiber.StatusInternalServerError
}

func statusMessage(nextErr error) string {
	if nextErr != nil {
		return nextErr.Error()
	}
	return ""
}

// requestAttrs reports the attributes that are known before routing. The route
// pattern is deliberately absent: it does not exist yet at this point.
func requestAttrs(c *fiber.Ctx) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		semconv.HTTPRequestMethodKey.String(c.Method()),
		semconv.URLPath(c.Path()),
		semconv.URLScheme(c.Protocol()),
	}
	if host := c.Hostname(); host != "" {
		attrs = append(attrs, semconv.ServerAddress(host))
	}
	if ua := c.Get(fiber.HeaderUserAgent); ua != "" {
		attrs = append(attrs, semconv.UserAgentOriginal(ua))
	}
	if ip := c.IP(); ip != "" {
		attrs = append(attrs, semconv.ClientAddress(ip))
	}
	if query := string(c.Request().URI().QueryString()); query != "" {
		attrs = append(attrs, semconv.URLQuery(query))
	}
	return attrs
}

var _ propagation.TextMapCarrier = headerCarrier{}
