// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package zap

import (
	"go.uber.org/zap/zapcore"

	"go.opentelemetry.io/otelc/pkg/hook"
	"go.opentelemetry.io/otelc/pkg/runtime"
)

const (
	instrumentationKey = "logs/zap"
	traceIDKey         = "trace_id"
	spanIDKey          = "span_id"
	fieldsParamIndex   = 1
)

type logEnabler struct{}

func (l logEnabler) Enable() bool {
	return runtime.Instrumented(instrumentationKey)
}

var enabler = logEnabler{}

func stringField(key, val string) zapcore.Field {
	return zapcore.Field{Key: key, Type: zapcore.StringType, String: val}
}

func BeforeCheckedEntryWrite(ictx hook.HookContext, ce *zapcore.CheckedEntry, fields ...zapcore.Field) {
	if !enabler.Enable() || ce == nil {
		return
	}

	traceID, spanID := runtime.GetTraceAndSpanID()
	if traceID == "" {
		return
	}

	// Copy so append cannot write into spare capacity on a reused caller slice.
	out := make([]zapcore.Field, len(fields), len(fields)+2)
	copy(out, fields)
	out = append(out, stringField(traceIDKey, traceID))
	if spanID != "" {
		out = append(out, stringField(spanIDKey, spanID))
	}
	ictx.SetParam(fieldsParamIndex, out)
}
