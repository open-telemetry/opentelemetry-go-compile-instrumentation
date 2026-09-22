// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package log

import (
	"bytes"
	"log"
	"strings"

	"go.opentelemetry.io/otelc/pkg/hook"
	"go.opentelemetry.io/otelc/pkg/runtime"
)

const (
	instrumentationKey = "logs/log"
	traceIDKey         = "trace_id"
	spanIDKey          = "span_id"
	traceIDMarker      = " " + traceIDKey + "="
	traceIDPrefix      = traceIDKey + "="
)

func hasTraceID(b []byte) bool {
	return bytes.HasPrefix(b, []byte(traceIDPrefix)) || bytes.Contains(b, []byte(traceIDMarker))
}

type logEnabler struct{}

func (l logEnabler) Enable() bool {
	return runtime.Instrumented(instrumentationKey)
}

var enabler = logEnabler{}

func BeforeLogOutput(
	ictx hook.HookContext,
	logger *log.Logger,
	pc uintptr,
	calldepth int,
	appendOutput func([]byte) []byte,
) {
	if !enabler.Enable() {
		return
	}

	newAppendOutput := func(b []byte) []byte {
		b = appendOutput(b)
		if len(b) == 0 {
			return b
		}

		if hasTraceID(b) {
			return b
		}

		traceID, spanID := runtime.GetTraceAndSpanID()
		if traceID == "" {
			return b
		}

		var sb strings.Builder
		sb.WriteString(" ")
		sb.WriteString(traceIDKey)
		sb.WriteString("=")
		sb.WriteString(traceID)

		if spanID != "" {
			sb.WriteString(" ")
			sb.WriteString(spanIDKey)
			sb.WriteString("=")
			sb.WriteString(spanID)
		}

		traceSuffix := sb.String()

		idx := len(b)
		for idx > 0 && (b[idx-1] == '\n' || b[idx-1] == '\r') {
			idx--
		}

		b = append(b[:idx], append([]byte(traceSuffix), b[idx:]...)...)
		return b
	}

	ictx.SetParam(3, newAppendOutput)
}
