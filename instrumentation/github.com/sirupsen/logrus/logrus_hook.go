// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package logrus

import (
	"strings"
	"sync"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/hook"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/runtime"
	"github.com/sirupsen/logrus"
)

const (
	instrumentationKey = "logs/logrus"
	traceIdKey         = "trace_id"
	spanIdKey          = "span_id"
)

type logEnabler struct{}

func (l logEnabler) Enable() bool {
	return runtime.Instrumented(instrumentationKey)
}

var enabler = logEnabler{}

var (
	hookInitMu    sync.Mutex
	hookInitMap   = make(map[*logrus.Logger]bool)
	fieldInitMap  = make(map[*logrus.Logger]bool)
	formatterInit = false
)

type traceHook struct{}

func (h *traceHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (h *traceHook) Fire(entry *logrus.Entry) error {
	if !enabler.Enable() {
		return nil
	}

	traceId, spanId := runtime.GetTraceAndSpanId()
	if traceId != "" {
		entry.Data[traceIdKey] = traceId
	}
	if spanId != "" {
		entry.Data[spanIdKey] = spanId
	}
	return nil
}

func AfterLogrusNew(ictx hook.HookContext, logger *logrus.Logger) {
	if !enabler.Enable() || logger == nil {
		return
	}

	hookInitMu.Lock()
	defer hookInitMu.Unlock()

	if hookInitMap[logger] {
		return
	}

	logger.AddHook(&traceHook{})
	hookInitMap[logger] = true
}

func AfterLogrusWithField(ictx hook.HookContext, entry *logrus.Entry) {
	if !enabler.Enable() || entry == nil || entry.Logger == nil {
		return
	}

	hookInitMu.Lock()
	defer hookInitMu.Unlock()

	if fieldInitMap[entry.Logger] {
		return
	}

	if entry.Logger.Hooks == nil {
		entry.Logger.Hooks = make(logrus.LevelHooks)
	}

	entry.Logger.AddHook(&traceHook{})
	fieldInitMap[entry.Logger] = true
}

func AfterLogrusSetFormatter(ictx hook.HookContext) {
	if !enabler.Enable() {
		return
	}

	hookInitMu.Lock()
	defer hookInitMu.Unlock()

	if formatterInit {
		return
	}

	std := logrus.StandardLogger()
	std.AddHook(&traceHook{})
	formatterInit = true
}

func BeforeLogrusEntryLog(ictx hook.HookContext, entry *logrus.Entry, level logrus.Level, args ...interface{}) {
	if !enabler.Enable() || args == nil {
		return
	}

	traceId, spanId := runtime.GetTraceAndSpanId()

	var newArgs []interface{}
	if traceId != "" {
		newArgs = append(newArgs, " "+traceIdKey+":", traceId)
	}
	if spanId != "" {
		newArgs = append(newArgs, " "+spanIdKey+":", spanId)
	}

	if len(newArgs) > 0 {
		for _, arg := range args {
			if str, ok := arg.(string); ok {
				if strings.Contains(str, traceIdKey) {
					return
				}
			}
		}
		args = append(args, newArgs...)
		ictx.SetParam(2, args)
	}
}
