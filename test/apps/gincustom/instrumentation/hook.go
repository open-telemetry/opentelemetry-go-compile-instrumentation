// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hook

import (
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/hook"
)

func afterNew(_ hook.HookContext, r *gin.Engine) {
	r.Use(func(c *gin.Context) {
		span := trace.SpanFromContext(c.Request.Context())
		if !span.IsRecording() {
			return
		}
		// add a custom span attribute to indicate that this is a custom instrumentation span
		span.SetAttributes(attribute.Bool("gin.otelc.custom", true))
		c.Next()
	})
}
