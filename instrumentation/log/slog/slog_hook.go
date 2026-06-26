// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package slog

import (
	"log/slog"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/hook"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/runtime"
)

const (
	instrumentationKey = "logs/slog"
	traceIDKey         = "trace_id"
	spanIDKey          = "span_id"
)

type logEnabler struct{}

func (l logEnabler) Enable() bool {
	return runtime.Instrumented(instrumentationKey)
}

var enabler = logEnabler{}

func AfterSlogNewRecord(ictx hook.HookContext, r slog.Record) {
	if !enabler.Enable() {
		return
	}

	traceID, spanID := runtime.GetTraceAndSpanID()
	if traceID == "" {
		return
	}

	var attrs []slog.Attr
	attrs = append(attrs, slog.String(traceIDKey, traceID))
	if spanID != "" {
		attrs = append(attrs, slog.String(spanIDKey, spanID))
	}

	r.AddAttrs(attrs...)
	ictx.SetReturnVal(0, r)
}
