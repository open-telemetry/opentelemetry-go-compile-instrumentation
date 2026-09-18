// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"github.com/gofiber/fiber/v2"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otelc/pkg/hook"
)

const (
	routeSetKey    = "otel.fiber.route.set"
	nextDepthKey   = "otel.fiber.next.depth"
	enabledDataKey = "otel.fiber.enabled"
)

// BeforeNext runs before (*fiber.Ctx).Next.
func BeforeNext(ictx hook.HookContext, c *fiber.Ctx) {
	enabled := enabler.Enable()
	ictx.SetKeyData(enabledDataKey, enabled)
	if !enabled || c == nil {
		return
	}

	d := c.Locals(nextDepthKey)
	if depth, ok := d.(int); ok {
		c.Locals(nextDepthKey, depth+1)
	} else {
		c.Locals(nextDepthKey, 1)
	}

	route := ""
	if r := c.Route(); r != nil {
		route = r.Path
	}
	if route == "" {
		return
	}

	if _, already := c.Locals(routeSetKey).(struct{}); already {
		return
	}

	span := trace.SpanFromContext(c.UserContext())
	if !span.IsRecording() {
		return
	}

	c.Locals(routeSetKey, struct{}{})

	span.SetName(c.Method() + " " + route)
	span.SetAttributes(semconv.HTTPRouteKey.String(route))

	logger.Debug("fiber route resolved", "route", route)
}

// AfterNext runs after (*fiber.Ctx).Next returns. nextErr is Next's return
// value: the error a downstream handler returned, before Fiber's error handler
// has turned it into a response status.
func AfterNext(ictx hook.HookContext, nextErr error) {
	enabled, _ := ictx.GetKeyData(enabledDataKey).(bool)
	if !enabled {
		return
	}

	c, ok := ictx.GetParam(0).(*fiber.Ctx)
	if !ok || c == nil {
		return
	}

	depth, _ := c.Locals(nextDepthKey).(int)
	if depth > 1 {
		c.Locals(nextDepthKey, depth-1)
		return
	}

	c.Locals(nextDepthKey, 0)

	span := trace.SpanFromContext(c.UserContext())
	if !span.IsRecording() {
		return
	}

	status := c.Response().StatusCode()
	span.SetAttributes(semconv.HTTPResponseStatusCodeKey.Int(status))

	// A handler error is the more precise signal: Fiber's error handler maps it
	// to a status only after Next returns, so the response may still read 200
	// here even though the request failed.
	if nextErr != nil {
		span.RecordError(nextErr)
		span.SetStatus(codes.Error, nextErr.Error())
		return
	}

	if status >= 500 {
		span.SetStatus(codes.Error, "")
	}
}
