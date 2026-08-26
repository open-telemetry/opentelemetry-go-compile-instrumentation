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

// AfterNext runs after (*fiber.Ctx).Next returns.
func AfterNext(ictx hook.HookContext) {
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
	if status >= 400 {
		if status >= 500 {
			span.SetStatus(codes.Error, "")
		}
		span.SetAttributes(semconv.HTTPResponseStatusCodeKey.Int(status))
	}
}
