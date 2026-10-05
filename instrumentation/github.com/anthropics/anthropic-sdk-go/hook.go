// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package anthropic

import (
	"sync"

	"github.com/anthropics/anthropic-sdk-go/option"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otelc/pkg/hook"
	"go.opentelemetry.io/otelc/pkg/runtime"
)

const (
	instrumentationName = "go.opentelemetry.io/otelc/instrumentation/github.com/anthropics/anthropic-sdk-go"
	instrumentationKey  = "ANTHROPIC"
)

var (
	logger            = runtime.Logger()
	tracer            trace.Tracer
	operationDuration metric.Float64Histogram
	initOnce          sync.Once
)

type anthropicEnabler struct{}

func (a anthropicEnabler) Enable() bool {
	return runtime.Instrumented(instrumentationKey)
}

var enabler = anthropicEnabler{}

func initInstrumentation() {
	initOnce.Do(func() {
		tracer = otel.GetTracerProvider().Tracer(
			instrumentationName,
			trace.WithInstrumentationVersion(runtime.ModuleVersion()),
		)

		meter := otel.GetMeterProvider().Meter(
			instrumentationName,
			metric.WithInstrumentationVersion(runtime.ModuleVersion()),
		)
		var err error
		operationDuration, err = meter.Float64Histogram(
			"gen_ai.client.operation.duration",
			metric.WithDescription("Duration of GenAI client operations."),
			metric.WithUnit("s"),
		)
		if err != nil {
			logger.Error("failed to create gen_ai.client.operation.duration histogram", "error", err)
		}

		logger.Info("Anthropic instrumentation initialized")
	})
}

func BeforeNewClient(ictx hook.HookContext, opts ...option.RequestOption) {
	if !enabler.Enable() {
		return
	}
	initInstrumentation()

	newOpts := make([]option.RequestOption, 0, len(opts)+1)
	newOpts = append(newOpts, option.WithMiddleware(OtelMiddleware()))
	newOpts = append(newOpts, opts...)
	ictx.SetParam(0, newOpts)
}
