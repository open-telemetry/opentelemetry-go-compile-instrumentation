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

func BeforeZerologEventMsg(_ hook.HookContext, event *zerolog.Event, _ string) {
	enrichEvent(event)
}

func BeforeZerologEventMsgf(_ hook.HookContext, event *zerolog.Event, _ string, _ ...interface{}) {
	enrichEvent(event)
}

func BeforeZerologEventMsgFunc(_ hook.HookContext, event *zerolog.Event, _ func() string) {
	enrichEvent(event)
}

func BeforeZerologEventSend(_ hook.HookContext, event *zerolog.Event) {
	enrichEvent(event)
}

func enrichEvent(event *zerolog.Event) {
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
