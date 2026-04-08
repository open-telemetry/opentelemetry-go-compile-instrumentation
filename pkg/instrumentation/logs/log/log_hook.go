// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package log

import (
	"log"
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/shared"
)

const (
	instrumentationKey = "logs/log"
	traceIdKey         = "trace_id"
	spanIdKey          = "span_id"
)

// logEnabler controls whether log instrumentation is enabled
type logEnabler struct{}

func (l logEnabler) Enable() bool {
	return shared.Instrumented(instrumentationKey)
}

var enabler = logEnabler{}

// BeforeLogOutput is called before (*Logger).output() to inject trace context into log message
func BeforeLogOutput(ictx inst.HookContext, logger *log.Logger, pc uintptr, calldepth int, appendOutput func([]byte) []byte) {
	if !enabler.Enable() {
		return
	}

	// Wrap the appendOutput function to inject trace context
	newAppendOutput := func(b []byte) []byte {
		b = appendOutput(b)
		if len(b) == 0 {
			return b
		}

		// Check if trace context is already in the output
		if strings.Contains(string(b), traceIdKey) {
			return b
		}

		traceId, spanId := shared.GetTraceAndSpanId()
		if traceId == "" {
			return b
		}

		var sb strings.Builder
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

		traceSuffix := sb.String()

		// Insert trace/span before trailing line breaks to avoid creating a new line
		// containing only trace/span.
		idx := len(b)
		for idx > 0 && (b[idx-1] == '\n' || b[idx-1] == '\r') {
			idx--
		}

		b = append(b[:idx], append([]byte(traceSuffix), b[idx:]...)...)
		return b
	}

	ictx.SetParam(3, newAppendOutput)
}
