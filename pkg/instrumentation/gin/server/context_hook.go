// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"github.com/gin-gonic/gin"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
)

// routeSetKey is stored on the gin.Context to prevent repeated span updates
// when multiple middleware layers call c.Next().
const routeSetKey = "otel.gin.route.set"

// BeforeNext runs before (*gin.Context).Next. By the time Next is called,
// gin's router has already matched the request to a route and populated
// c.FullPath(). We use this to update the span name from the initial
// "METHOD" to "METHOD /route/pattern" and record the http.route attribute.
func BeforeNext(ictx inst.HookContext, c *gin.Context) {
	if !serverEnabler.Enable() {
		return
	}
	if c == nil || c.Request == nil {
		return
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
	c.Set(routeSetKey, struct{}{})

	span := trace.SpanFromContext(c.Request.Context())
	if !span.IsRecording() {
		return
	}

	span.SetName(c.Request.Method + " " + route)
	span.SetAttributes(semconv.HTTPRouteKey.String(route))

	logger.Debug("gin route resolved", "route", route)
}
