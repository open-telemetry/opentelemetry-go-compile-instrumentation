// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package logrus

import (
	"strings"
	"sync"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/shared"
	"github.com/sirupsen/logrus"
)

const (
	instrumentationKey = "logs/logrus"
	traceIdKey         = "trace_id"
	spanIdKey          = "span_id"
)

// logEnabler controls whether logrus instrumentation is enabled
type logEnabler struct{}

func (l logEnabler) Enable() bool {
	return shared.Instrumented(instrumentationKey)
}

var enabler = logEnabler{}

// Track which loggers have been initialized with our hook
var (
	hookInitMu    sync.Mutex
	hookInitMap   = make(map[*logrus.Logger]bool)
	fieldInitMap  = make(map[*logrus.Logger]bool)
	formatterInit = false
)

// traceHook is a logrus hook that adds trace context to log entries
type traceHook struct{}

func (h *traceHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (h *traceHook) Fire(entry *logrus.Entry) error {
	if !enabler.Enable() {
		return nil
	}

	traceId, spanId := shared.GetTraceAndSpanId()
	if traceId != "" {
		entry.Data[traceIdKey] = traceId
	}
	if spanId != "" {
		entry.Data[spanIdKey] = spanId
	}
	return nil
}

// AfterLogrusNew is called after logrus.New() to add our hook
func AfterLogrusNew(ictx inst.HookContext, logger *logrus.Logger) {
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

// AfterLogrusWithField is called after logrus.WithField() to ensure hook is added
func AfterLogrusWithField(ictx inst.HookContext, entry *logrus.Entry) {
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

// AfterLogrusSetFormatter is called after SetFormatter to add hook to standard logger
func AfterLogrusSetFormatter(ictx inst.HookContext) {
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

// BeforeLogrusEntryLog is called before (*Entry).Log() to add trace context to log args
func BeforeLogrusEntryLog(ictx inst.HookContext, entry *logrus.Entry, level logrus.Level, args ...interface{}) {
	if !enabler.Enable() || args == nil {
		return
	}

	traceId, spanId := shared.GetTraceAndSpanId()

	var newArgs []interface{}
	if traceId != "" {
		newArgs = append(newArgs, " "+traceIdKey+":", traceId)
	}
	if spanId != "" {
		newArgs = append(newArgs, " "+spanIdKey+":", spanId)
	}

	if len(newArgs) > 0 {
		// Check if trace context is already in args
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
