// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package logrus

import (
	goruntime "runtime"
	"sync"
	"weak"

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

// initialized starts nil rather than being made here. AfterLogrusNew and
// AfterLogrusWithField are wired via //go:linkname, which doesn't create a
// normal Go import edge, so this package's own var initializers are not
// guaranteed to have run by the time a hook fires (for example when
// logrus's own "var std = New()" triggers AfterLogrusNew during logrus's
// package init). Assigning through make() here would race that init and
// could leave a hook writing into a nil map. ensureTraceHook lazily
// initializes the map under initMu instead, which is safe regardless of
// init order.
//
// One map now backs all three hooks below (AfterLogrusNew,
// AfterLogrusWithField, AfterLogrusSetFormatter) instead of a separate map
// per hook. A logger commonly goes through more than one of these paths,
// for example logrus.New() followed by a WithField() call on the result;
// with a map per path neither guard could see what the other had already
// done, so the same logger got a second traceHook attached. Sharing one
// map closes that gap.
//
// The map is keyed by a weak pointer rather than the *logrus.Logger itself.
// A strong-pointer key would keep every logger instrumentation has ever
// seen reachable for the life of the process, which is an unbounded leak
// for any program that creates short-lived loggers (per-request loggers,
// tests, etc.). The weak key lets a logger be collected normally, and the
// runtime.AddCleanup call in ensureTraceHook drops the now-dead entry when
// that happens.
var (
	initMu      sync.Mutex
	initialized map[weak.Pointer[logrus.Logger]]struct{}
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

// ensureTraceHook attaches traceHook to logger the first time it is seen,
// regardless of which of AfterLogrusNew, AfterLogrusWithField, or
// AfterLogrusSetFormatter found it first.
func ensureTraceHook(logger *logrus.Logger) {
	wp := weak.Make(logger)

	initMu.Lock()
	defer initMu.Unlock()

	if initialized == nil {
		initialized = make(map[weak.Pointer[logrus.Logger]]struct{})
	}
	if _, ok := initialized[wp]; ok {
		return
	}

	if logger.Hooks == nil {
		logger.Hooks = make(logrus.LevelHooks)
	}
	logger.AddHook(&traceHook{})
	initialized[wp] = struct{}{}

	goruntime.AddCleanup(logger, forgetLogger, wp)
}

func forgetLogger(wp weak.Pointer[logrus.Logger]) {
	initMu.Lock()
	defer initMu.Unlock()
	delete(initialized, wp)
}

func AfterLogrusNew(ictx hook.HookContext, logger *logrus.Logger) {
	if !enabler.Enable() || logger == nil {
		return
	}
	ensureTraceHook(logger)
}

func AfterLogrusWithField(ictx hook.HookContext, entry *logrus.Entry) {
	if !enabler.Enable() || entry == nil || entry.Logger == nil {
		return
	}
	ensureTraceHook(entry.Logger)
}

func AfterLogrusSetFormatter(ictx hook.HookContext) {
	if !enabler.Enable() {
		return
	}
	ensureTraceHook(logrus.StandardLogger())
}
