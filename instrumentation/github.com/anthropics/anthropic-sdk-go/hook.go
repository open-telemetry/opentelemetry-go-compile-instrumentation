// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package anthropic

import (
	"os"
	"strings"
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
	// sdkOpenTelemetryEnv is the environment variable checked by
	// anthropic-sdk-go v1.81.0+ to toggle its built-in OpenTelemetry tracing.
	sdkOpenTelemetryEnv = "ANTHROPIC_OPEN_TELEMETRY"
)

var (
	logger            = runtime.Logger()
	tracer            trace.Tracer
	operationDuration metric.Float64Histogram
	initOnce          sync.Once
	envMu             sync.Mutex
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

// disableSDKOpenTelemetry sets ANTHROPIC_OPEN_TELEMETRY=false when the
// variable is not already set, preventing anthropic-sdk-go v1.81.0+ from
// emitting duplicate spans alongside otelc's GenAI instrumentation.
func disableSDKOpenTelemetry() {
	envMu.Lock()
	defer envMu.Unlock()
	if os.Getenv(sdkOpenTelemetryEnv) == "" {
		if err := os.Setenv(sdkOpenTelemetryEnv, "false"); err != nil {
			logger.Error("failed to set "+sdkOpenTelemetryEnv, "error", err)
		}
	}
}

func BeforeNewClient(ictx hook.HookContext, opts ...option.RequestOption) {
	if !enabler.Enable() {
		return
	}
	// If the user explicitly opted into the SDK's built-in tracing via
	// ANTHROPIC_OPEN_TELEMETRY=true, skip otelc's middleware to avoid
	// emitting duplicate spans.
	if strings.EqualFold(os.Getenv(sdkOpenTelemetryEnv), "true") {
		return
	}
	initInstrumentation()
	disableSDKOpenTelemetry()

	newOpts := make([]option.RequestOption, 0, len(opts)+1)
	newOpts = append(newOpts, option.WithMiddleware(OtelMiddleware()))
	newOpts = append(newOpts, opts...)
	ictx.SetParam(0, newOpts)
}
