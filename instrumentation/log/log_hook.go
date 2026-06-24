// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package log

import (
	"log"
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/hook"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/runtime"
)

const (
	instrumentationKey = "logs/log"
	traceIdKey         = "trace_id"
	spanIdKey          = "span_id"
)

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

		if strings.Contains(string(b), traceIdKey) {
			return b
		}

		traceId, spanId := runtime.GetTraceAndSpanId()
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

		idx := len(b)
		for idx > 0 && (b[idx-1] == '\n' || b[idx-1] == '\r') {
			idx--
		}

		b = append(b[:idx], append([]byte(traceSuffix), b[idx:]...)...)
		return b
	}

	ictx.SetParam(3, newAppendOutput)
}
