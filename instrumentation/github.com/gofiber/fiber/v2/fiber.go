// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otelc/pkg/runtime"
)

const (
	instrumentationName = "go.opentelemetry.io/otelc/instrumentation/github.com/gofiber/fiber/v2"

	// instrumentationKey names this instrumentation for the runtime
	// OTEL_GO_ENABLED_INSTRUMENTATIONS / OTEL_GO_DISABLED_INSTRUMENTATIONS lists.
	instrumentationKey = "FIBER"
)

var (
	logger     = runtime.Logger()
	tracer     trace.Tracer
	propagator propagation.TextMapPropagator
	initOnce   sync.Once
)

func initInstrumentation() {
	initOnce.Do(func() {
		tracer = otel.GetTracerProvider().Tracer(
			instrumentationName,
			trace.WithInstrumentationVersion(runtime.ModuleVersion()),
		)
		propagator = otel.GetTextMapPropagator()
		logger.Info("Fiber server instrumentation initialized")
	})
}

// fiberEnabler controls whether fiber instrumentation is enabled
type fiberEnabler struct{}

func (f fiberEnabler) Enable() bool {
	return runtime.Instrumented(instrumentationKey)
}

var enabler = fiberEnabler{}
