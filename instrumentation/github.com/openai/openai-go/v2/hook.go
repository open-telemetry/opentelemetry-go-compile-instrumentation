// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package v2

import (
	"runtime/debug"
	"sync"

	"github.com/openai/openai-go/v2/option"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/hook"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/runtime"
)

const (
	instrumentationName = "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/github.com/openai/openai-go/v2"
	instrumentationKey  = "OPENAI"
)

var (
	logger   = runtime.Logger()
	tracer   trace.Tracer
	initOnce sync.Once
)

type openaiEnabler struct{}

func (o openaiEnabler) Enable() bool {
	return runtime.Instrumented(instrumentationKey)
}

var enabler = openaiEnabler{}

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

func initInstrumentation() {
	initOnce.Do(func() {
		version := moduleVersion()
		if err := runtime.SetupOTelSDK(instrumentationName, version); err != nil {
			logger.Error("failed to setup OTel SDK", "error", err)
		}
		tracer = otel.GetTracerProvider().Tracer(
			instrumentationName,
			trace.WithInstrumentationVersion(version),
		)

		if err := runtime.StartRuntimeMetrics(); err != nil {
			logger.Error("failed to start runtime metrics", "error", err)
		}

		logger.Info("OpenAI v2 instrumentation initialized")
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
