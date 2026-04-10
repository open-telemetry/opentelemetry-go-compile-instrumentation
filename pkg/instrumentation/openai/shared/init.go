// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package shared contains the HTTP middleware and initialization code used by
// all OpenAI SDK versions (v1, v2, v3). Each version package is a thin wrapper
// that injects this middleware via option.WithMiddleware, so the attribute
// extraction and metric recording logic lives here, in one place, with no
// dependency on any specific openai-go module.
package shared

import (
	"context"
	"log/slog"
	"runtime/debug"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/semconv/v1.37.0/genaiconv"
	"go.opentelemetry.io/otel/trace"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/shared"
)

const (
	instrumentationName = "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/openai"
	instrumentationKey  = "OPENAI"
)

var (
	logger       *slog.Logger
	tracer       trace.Tracer
	durationHist genaiconv.ClientOperationDuration
	tokenUsage   genaiconv.ClientTokenUsage
	initOnce     sync.Once
)

// Enabled reports whether OpenAI instrumentation is active. Checked on every
// hook and middleware invocation so operators can toggle it at runtime via
// OTEL_GO_ENABLED_INSTRUMENTATIONS / OTEL_GO_DISABLED_INSTRUMENTATIONS.
func Enabled() bool {
	return shared.Instrumented(instrumentationKey)
}

func moduleVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

// initInstrumentation sets up the tracer, meter, and metric instruments on
// first use. It is guarded by sync.Once so multiple version packages can call
// it concurrently without duplicate setup.
func initInstrumentation() {
	initOnce.Do(func() {
		logger = shared.Logger()
		version := moduleVersion()
		if err := shared.SetupOTelSDK("go.opentelemetry.io/compile-instrumentation/openai", version); err != nil {
			logger.Error("failed to setup OTel SDK", "error", err)
		}
		tracer = otel.GetTracerProvider().Tracer(
			instrumentationName,
			trace.WithInstrumentationVersion(version),
		)
		meter := otel.GetMeterProvider().Meter(
			instrumentationName,
			metric.WithInstrumentationVersion(version),
		)
		var err error
		durationHist, err = genaiconv.NewClientOperationDuration(meter)
		if err != nil {
			logger.Error("failed to create duration histogram", "error", err)
		}
		tokenUsage, err = genaiconv.NewClientTokenUsage(meter)
		if err != nil {
			logger.Error("failed to create token usage histogram", "error", err)
		}
		if err := shared.StartRuntimeMetrics(); err != nil {
			logger.Error("failed to start runtime metrics", "error", err)
		}
		logger.Info("OpenAI client instrumentation initialized")
	})
}

// recordDuration records the gen_ai.client.operation.duration histogram.
func recordDuration(ctx context.Context, operation, model string, durationSec float64, err error) {
	attrs := []attribute.KeyValue{
		durationHist.AttrRequestModel(model),
	}
	if err != nil {
		attrs = append(attrs, durationHist.AttrErrorType(genaiconv.ErrorTypeOther))
	}
	durationHist.Record(ctx, durationSec,
		genaiconv.OperationNameAttr(operation),
		genaiconv.ProviderNameOpenAI,
		attrs...,
	)
}

// recordTokenUsage records the gen_ai.client.token.usage histogram for the
// prompt (input) and completion (output) token counts returned by the API.
func recordTokenUsage(ctx context.Context, operation, model string, inputTokens, outputTokens int64) {
	modelAttr := tokenUsage.AttrRequestModel(model)
	if inputTokens > 0 {
		tokenUsage.Record(ctx, inputTokens,
			genaiconv.OperationNameAttr(operation),
			genaiconv.ProviderNameOpenAI,
			genaiconv.TokenTypeInput,
			modelAttr,
		)
	}
	if outputTokens > 0 {
		tokenUsage.Record(ctx, outputTokens,
			genaiconv.OperationNameAttr(operation),
			genaiconv.ProviderNameOpenAI,
			genaiconv.TokenTypeOutput,
			modelAttr,
		)
	}
}
