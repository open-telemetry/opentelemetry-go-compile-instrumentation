// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package slog

import (
	"context"
	"log/slog"
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/shared"
)

const (
	instrumentationKey = "logs/slog"
	traceIdKey         = "trace_id"
	spanIdKey          = "span_id"
)

// logEnabler controls whether slog instrumentation is enabled
type logEnabler struct{}

func (l logEnabler) Enable() bool {
	return shared.Instrumented(instrumentationKey)
}

var enabler = logEnabler{}

// BeforeSlogLog is called before (*Logger).log() to inject trace context into log message
func BeforeSlogLog(
	ictx inst.HookContext,
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

	// Check if trace context is already in the message
	if strings.Contains(msg, traceIdKey) {
		return
	}

	traceId, spanId := shared.GetTraceAndSpanId()
	if traceId == "" {
		return
	}

	// Append trace context to message
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

// AfterSlogNewRecord is called after NewRecord to add trace attributes to the record
func AfterSlogNewRecord(ictx inst.HookContext, r slog.Record) {
	if !enabler.Enable() {
		return
	}

	traceId, spanId := shared.GetTraceAndSpanId()
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
