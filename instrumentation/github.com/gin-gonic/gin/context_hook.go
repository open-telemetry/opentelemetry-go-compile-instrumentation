// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/hook"
)

// routeSetKey is stored on the gin.Context to prevent repeated span updates
// when multiple middleware layers call c.Next(). The key is reserved by this
// package; user middleware must not set or read it.
const (
	routeSetKey  = "otel.gin.route.set"
	nextDepthKey = "otel.gin.next.depth"
)

// BeforeNext runs before (*gin.Context).Next. By the time Next is called,
// gin's router has already matched the request to a route and populated
// c.FullPath(). We use this to update the span name from the initial
// "METHOD" to "METHOD /route/pattern" and record the http.route attribute.
func BeforeNext(ictx hook.HookContext, c *gin.Context) {
	if c == nil || c.Request == nil {
		return
	}

	if d, exists := c.Get(nextDepthKey); exists {
		if depth, ok := d.(int); ok {
			c.Set(nextDepthKey, depth+1)
		} else {
			c.Set(nextDepthKey, 1)
		}
	} else {
		c.Set(nextDepthKey, 1)
	}

	route := c.FullPath()
	if route == "" {
		// No route matched (e.g. 404). Leave the span name as the method only.
		return
	}

	// c.Next() is called by each middleware in the chain, so this hook fires
	// multiple times per request. Only the first call needs to update the span.
	if _, already := c.Get(routeSetKey); already {
		return
	}

	span := trace.SpanFromContext(c.Request.Context())
	if !span.IsRecording() {
		return
	}

	// Set the gate only after confirming we have a recording span to update.
	// Otherwise a non-recording first call would burn the gate and block a
	// later recording span on the same request from being enriched.
	c.Set(routeSetKey, struct{}{})

	span.SetName(c.Request.Method + " " + route)
	span.SetAttributes(semconv.HTTPRouteKey.String(route))

	logger.Debug("gin route resolved", "route", route)
}

// AfterNext runs after (*gin.Context).Next returns. It records any errors
// accumulated via c.Error() during request handling.
func AfterNext(ictx hook.HookContext) {
	c, ok := ictx.GetParam(0).(*gin.Context)
	if !ok || c == nil || c.Request == nil {
		return
	}

	d, _ := c.Get(nextDepthKey)
	depth, _ := d.(int)
	depth--
	c.Set(nextDepthKey, depth)

	if depth > 0 {
		return
	}

	if len(c.Errors) == 0 {
		return
	}

	span := trace.SpanFromContext(c.Request.Context())
	if !span.IsRecording() {
		return
	}

	span.SetStatus(codes.Error, c.Errors.String())
	for _, e := range c.Errors {
		span.RecordError(e.Err)
	}
}
