// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package v1

import (
	"sync"

	"github.com/openai/openai-go/option"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/hook"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/runtime"
)

const (
	instrumentationName = "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/github.com/openai/openai-go"
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

func initInstrumentation() {
	initOnce.Do(func() {
		tracer = otel.GetTracerProvider().Tracer(
			instrumentationName,
			trace.WithInstrumentationVersion(runtime.ModuleVersion()),
		)
		logger.Info("OpenAI v1 instrumentation initialized")
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
