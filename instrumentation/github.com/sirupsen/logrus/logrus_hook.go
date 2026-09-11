// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package logrus

import (
	"sync"

	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otelc/pkg/hook"
	"go.opentelemetry.io/otelc/pkg/runtime"
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

// hookInitMap and fieldInitMap start nil rather than being initialized here.
// AfterLogrusNew and AfterLogrusWithField are wired via //go:linkname, which
// doesn't create a normal Go import edge, so this package's own var
// initializers are not guaranteed to have run by the time a hook fires (for
// example when logrus's own "var std = New()" triggers AfterLogrusNew during
// logrus's package init). Assigning through make() here would race that
// init and could leave the hook writing into a nil map. Each hook lazily
// initializes its map under hookInitMu instead, which is safe regardless of
// init order.
var (
	hookInitMu    sync.Mutex
	hookInitMap   map[*logrus.Logger]bool
	fieldInitMap  map[*logrus.Logger]bool
	formatterInit bool
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

	if hookInitMap == nil {
		hookInitMap = make(map[*logrus.Logger]bool)
	}
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

	if fieldInitMap == nil {
		fieldInitMap = make(map[*logrus.Logger]bool)
	}
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
