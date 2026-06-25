// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package logrus

import (
	"sync"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/hook"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/runtime"
	"github.com/sirupsen/logrus"
)

const (
	instrumentationKey = "logs/logrus"
	traceIDKey         = "trace_id"
	spanIDKey          = "span_id"
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

	traceID, spanID := runtime.GetTraceAndSpanID()
	if traceID != "" {
		entry.Data[traceIDKey] = traceID
	}
	if spanID != "" {
		entry.Data[spanIDKey] = spanID
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
