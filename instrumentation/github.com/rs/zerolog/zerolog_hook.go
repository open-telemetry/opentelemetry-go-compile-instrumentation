// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package zerolog

import (
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otelc/pkg/hook"
	"go.opentelemetry.io/otelc/pkg/runtime"
)

const (
	instrumentationKey = "logs/zerolog"
	traceIDKey         = "trace_id"
	spanIDKey          = "span_id"
)

type logEnabler struct{}

func (l logEnabler) Enable() bool {
	return runtime.Instrumented(instrumentationKey)
}

var enabler = logEnabler{}

type traceHook struct{}

func (traceHook) Run(event *zerolog.Event, level zerolog.Level, message string) {
	if !enabler.Enable() || event == nil {
		return
	}

	traceID, spanID := runtime.GetTraceAndSpanID()
	if traceID == "" {
		return
	}

	event.Str(traceIDKey, traceID)
	if spanID != "" {
		event.Str(spanIDKey, spanID)
	}
}

func BeforeZerologNewEvent(_ hook.HookContext, logger *zerolog.Logger, _ zerolog.Level, _ func(string)) {
	if !enabler.Enable() || logger == nil {
		return
	}

	logger.OtelcInitOnce.Do(func() {
		*logger = logger.Hook(traceHook{})
	})
}
