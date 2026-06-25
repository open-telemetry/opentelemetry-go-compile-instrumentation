// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package slog

import (
	"context"
	"log/slog"
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/hook"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/runtime"
)

const (
	instrumentationKey = "logs/slog"
	traceIdKey         = "trace_id"
	spanIdKey          = "span_id"
)

type logEnabler struct{}

func (l logEnabler) Enable() bool {
	return runtime.Instrumented(instrumentationKey)
}

var enabler = logEnabler{}

func BeforeSlogLog(
	ictx hook.HookContext,
	logger *slog.Logger,
	ctx context.Context,
	level slog.Level,
	msg string,
	args ...any,
) {
	if !enabler.Enable() {
		return
	}

	if msg == "" {
		return
	}

	if strings.Contains(msg, traceIdKey) {
		return
	}

	traceId, spanId := runtime.GetTraceAndSpanId()
	if traceId == "" {
		return
	}

	var sb strings.Builder
	sb.WriteString(msg)
	sb.WriteString(" ")
	sb.WriteString(traceIdKey)
	sb.WriteString("=")
	sb.WriteString(traceId)

	if spanId != "" {
		sb.WriteString(" ")
		sb.WriteString(spanIdKey)
		sb.WriteString("=")
		sb.WriteString(spanId)
	}

	ictx.SetParam(4, sb.String())
}

func AfterSlogNewRecord(ictx hook.HookContext, r slog.Record) {
	if !enabler.Enable() {
		return
	}

	traceId, spanId := runtime.GetTraceAndSpanId()
	if traceId == "" {
		return
	}

	var attrs []slog.Attr
	attrs = append(attrs, slog.String(traceIdKey, traceId))
	if spanId != "" {
		attrs = append(attrs, slog.String(spanIdKey, spanId))
	}

	r.AddAttrs(attrs...)
	ictx.SetReturnVal(0, r)
}
