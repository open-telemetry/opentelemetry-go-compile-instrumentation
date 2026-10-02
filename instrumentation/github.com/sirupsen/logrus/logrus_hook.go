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

// initialized is created lazily in ensureTraceHook, not here, because the hooks
// run via go:linkname and can fire before this package's var initializers.
// One map is shared by all three hooks so a logger that goes through more than
// one of them only gets a single traceHook. It is keyed by weak pointer so
// loggers can still be garbage collected.
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

	addTraceHook(logger)
	initialized[wp] = struct{}{}

	goruntime.AddCleanup(logger, forgetLogger, wp)
}

// addTraceHook adds traceHook to logger, going only through logrus's own
// locked accessors (AddHook, ReplaceHooks) rather than logger.Hooks
// directly. A *logrus.Logger built via a struct literal instead of
// logrus.New() starts with a nil Hooks map, and AddHook panics writing into
// a nil map, so that case needs to be handled somehow. Checking
// logger.Hooks == nil and assigning to it here would read and write that
// field without the logger's own (unexported) mutex, racing any concurrent
// caller that goes through it, such as ReplaceHooks. Recovering from the
// one panic AddHook can throw and retrying through ReplaceHooks keeps every
// touch of logger.Hooks behind that mutex instead.
func addTraceHook(logger *logrus.Logger) {
	defer func() {
		if recover() != nil {
			logger.ReplaceHooks(make(logrus.LevelHooks))
			logger.AddHook(&traceHook{})
		}
	}()
	logger.AddHook(&traceHook{})
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
