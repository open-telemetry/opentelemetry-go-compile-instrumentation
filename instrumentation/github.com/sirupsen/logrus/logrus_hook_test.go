// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package logrus

import (
	goruntime "runtime"
	"testing"
	"time"
	"weak"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/pkg/hook/hooktest"
	"go.opentelemetry.io/otelc/pkg/runtime"
)

func resetHookState() {
	initMu.Lock()
	defer initMu.Unlock()
	initialized = nil
}

func hasTraceHook(logger *logrus.Logger) bool {
	return countTraceHooks(logger) > 0
}

func countTraceHooks(logger *logrus.Logger) int {
	count := 0
	for _, hooks := range logger.Hooks {
		for _, h := range hooks {
			if _, ok := h.(*traceHook); ok {
				count++
			}
		}
	}
	return count
}

func TestLogEnabler_Enable(t *testing.T) {
	tests := []struct {
		name         string
		enabledList  string
		disabledList string
		expected     bool
	}{
		{
			name:     "default enabled",
			expected: true,
		},
		{
			name:        "explicitly enabled",
			enabledList: "logs/logrus,logs/slog",
			expected:    true,
		},
		{
			name:        "not in enabled list",
			enabledList: "logs/slog",
			expected:    false,
		},
		{
			name:         "explicitly disabled",
			disabledList: "logs/logrus",
			expected:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.enabledList != "" {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", tt.enabledList)
			}
			if tt.disabledList != "" {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", tt.disabledList)
			}

			e := logEnabler{}
			result := e.Enable()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTraceHook_Levels(t *testing.T) {
	h := &traceHook{}
	levels := h.Levels()
	assert.Equal(t, logrus.AllLevels, levels)
}

func TestTraceHook_Fire_Disabled(t *testing.T) {
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "logs/logrus")

	h := &traceHook{}
	entry := &logrus.Entry{Data: logrus.Fields{}}
	err := h.Fire(entry)
	assert.NoError(t, err)
	assert.Empty(t, entry.Data)
}

func TestTraceHook_Fire_WithTraceContext(t *testing.T) {
	runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
		return "abc123traceId", "def456spanId"
	})
	defer runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
		return "", ""
	})

	h := &traceHook{}
	entry := &logrus.Entry{Data: logrus.Fields{}}
	err := h.Fire(entry)
	assert.NoError(t, err)
	assert.Equal(t, "abc123traceId", entry.Data["trace_id"])
	assert.Equal(t, "def456spanId", entry.Data["span_id"])
}

func TestTraceHook_Fire_WithTraceIDOnly(t *testing.T) {
	runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
		return "abc123traceId", ""
	})
	defer runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
		return "", ""
	})

	h := &traceHook{}
	entry := &logrus.Entry{Data: logrus.Fields{}}
	err := h.Fire(entry)
	assert.NoError(t, err)
	assert.Equal(t, "abc123traceId", entry.Data["trace_id"])
	_, hasSpanID := entry.Data["span_id"]
	assert.False(t, hasSpanID)
}

func TestTraceHook_Fire_NoTraceContext(t *testing.T) {
	runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
		return "", ""
	})

	h := &traceHook{}
	entry := &logrus.Entry{Data: logrus.Fields{}}
	err := h.Fire(entry)
	assert.NoError(t, err)
	assert.Empty(t, entry.Data)
}

// TestAfterLogrusNew_NilMapAtCallTime covers the AfterLogrusNew init-order
// panic (see #1028): the hook is wired via //go:linkname, so it can fire
// before this package's own var initializers have run, meaning
// initialized can still be nil the moment the hook is invoked. It must
// lazily initialize the map rather than panic on a nil-map write.
func TestAfterLogrusNew_NilMapAtCallTime(t *testing.T) {
	initMu.Lock()
	initialized = nil
	initMu.Unlock()
	t.Cleanup(resetHookState)

	ictx := hooktest.NewMockHookContext()
	logger := logrus.New()
	assert.NotPanics(t, func() {
		AfterLogrusNew(ictx, logger)
	})

	assert.True(t, hasTraceHook(logger))
}

// TestAfterLogrusWithField_NilMapAtCallTime is the AfterLogrusWithField
// counterpart of TestAfterLogrusNew_NilMapAtCallTime: initialized can also
// still be nil when the hook fires.
func TestAfterLogrusWithField_NilMapAtCallTime(t *testing.T) {
	initMu.Lock()
	initialized = nil
	initMu.Unlock()
	t.Cleanup(resetHookState)

	ictx := hooktest.NewMockHookContext()
	logger := logrus.New()
	entry := &logrus.Entry{Logger: logger, Data: logrus.Fields{}}
	assert.NotPanics(t, func() {
		AfterLogrusWithField(ictx, entry)
	})

	assert.True(t, hasTraceHook(logger))
}

// TestEnsureTraceHook_SharedAcrossPaths covers #1002: hookInitMap and
// fieldInitMap used to be tracked separately, so a logger that went
// through AfterLogrusNew and then AfterLogrusWithField (an ordinary
// sequence: create a logger, then attach a field to it) got traceHook
// attached twice, once per path. Every log line then ran the hook twice.
// AfterLogrusNew and AfterLogrusWithField must share one guard so the
// second path recognizes a logger the first path already handled.
func TestEnsureTraceHook_SharedAcrossPaths(t *testing.T) {
	resetHookState()

	ictx := hooktest.NewMockHookContext()
	logger := logrus.New()
	entry := &logrus.Entry{Logger: logger, Data: logrus.Fields{}}

	AfterLogrusNew(ictx, logger)
	AfterLogrusWithField(ictx, entry)

	assert.Equal(t, len(logrus.AllLevels), countTraceHooks(logger))
}

// TestEnsureTraceHook_SharedAcrossPaths_ReverseOrder is the same case as
// TestEnsureTraceHook_SharedAcrossPaths with the two hooks firing in the
// opposite order.
func TestEnsureTraceHook_SharedAcrossPaths_ReverseOrder(t *testing.T) {
	resetHookState()

	ictx := hooktest.NewMockHookContext()
	logger := logrus.New()
	entry := &logrus.Entry{Logger: logger, Data: logrus.Fields{}}

	AfterLogrusWithField(ictx, entry)
	AfterLogrusNew(ictx, logger)

	assert.Equal(t, len(logrus.AllLevels), countTraceHooks(logger))
}

// TestEnsureTraceHook_ForgetsCollectedLogger covers the other half of
// #1002: initialized used to be keyed by *logrus.Logger, so every logger
// instrumentation ever saw stayed in the map, and reachable, for the life
// of the process. A program that creates many short-lived loggers (one per
// request, one per test, ...) leaked memory without bound. The map is now
// keyed by a weak pointer with a cleanup that removes the entry once the
// logger is collected, so a logger that is no longer referenced anywhere
// else should eventually drop out of the map on its own.
func TestEnsureTraceHook_ForgetsCollectedLogger(t *testing.T) {
	resetHookState()

	newTrackedLogger := func() weak.Pointer[logrus.Logger] {
		ictx := hooktest.NewMockHookContext()
		logger := logrus.New()
		AfterLogrusNew(ictx, logger)
		return weak.Make(logger)
	}
	wp := newTrackedLogger()

	initMu.Lock()
	_, tracked := initialized[wp]
	initMu.Unlock()
	require.True(t, tracked, "logger should be tracked right after AfterLogrusNew")

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		goruntime.GC()

		initMu.Lock()
		_, stillTracked := initialized[wp]
		initMu.Unlock()
		if !stillTracked {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("logger entry was never forgotten after the logger became unreachable")
}

func TestAfterLogrusNew_Disabled(t *testing.T) {
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "logs/logrus")
	resetHookState()

	ictx := hooktest.NewMockHookContext()
	logger := logrus.New()
	AfterLogrusNew(ictx, logger)
	assert.Empty(t, logger.Hooks)
}

func TestAfterLogrusNew_NilLogger(t *testing.T) {
	ictx := hooktest.NewMockHookContext()
	AfterLogrusNew(ictx, nil)
}

func TestAfterLogrusNew_Enabled(t *testing.T) {
	resetHookState()

	ictx := hooktest.NewMockHookContext()
	logger := logrus.New()
	AfterLogrusNew(ictx, logger)

	hasHook := false
	for _, hooks := range logger.Hooks {
		for _, h := range hooks {
			if _, ok := h.(*traceHook); ok {
				hasHook = true
				break
			}
		}
	}
	assert.True(t, hasHook)
}

func TestAfterLogrusNew_Idempotent(t *testing.T) {
	resetHookState()

	ictx := hooktest.NewMockHookContext()
	logger := logrus.New()
	AfterLogrusNew(ictx, logger)
	AfterLogrusNew(ictx, logger)

	count := 0
	for _, hooks := range logger.Hooks {
		for _, h := range hooks {
			if _, ok := h.(*traceHook); ok {
				count++
			}
		}
	}
	assert.Equal(t, len(logrus.AllLevels), count)
}

func TestAfterLogrusWithField_Disabled(t *testing.T) {
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "logs/logrus")
	resetHookState()

	ictx := hooktest.NewMockHookContext()
	logger := logrus.New()
	entry := &logrus.Entry{Logger: logger}
	AfterLogrusWithField(ictx, entry)
	assert.Empty(t, logger.Hooks)
}

func TestAfterLogrusWithField_Enabled(t *testing.T) {
	resetHookState()

	ictx := hooktest.NewMockHookContext()
	logger := logrus.New()
	entry := &logrus.Entry{Logger: logger, Data: logrus.Fields{}}
	AfterLogrusWithField(ictx, entry)

	hasHook := false
	for _, hooks := range logger.Hooks {
		for _, h := range hooks {
			if _, ok := h.(*traceHook); ok {
				hasHook = true
				break
			}
		}
	}
	assert.True(t, hasHook)
}

func TestAfterLogrusWithField_NilEntry(t *testing.T) {
	resetHookState()
	ictx := hooktest.NewMockHookContext()
	AfterLogrusWithField(ictx, nil)
}

func TestAfterLogrusWithField_NilLogger(t *testing.T) {
	resetHookState()
	ictx := hooktest.NewMockHookContext()
	entry := &logrus.Entry{Logger: nil}
	AfterLogrusWithField(ictx, entry)
}

func TestAfterLogrusSetFormatter_Disabled(t *testing.T) {
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "logs/logrus")
	resetHookState()

	ictx := hooktest.NewMockHookContext()
	AfterLogrusSetFormatter(ictx)
}

func TestAfterLogrusSetFormatter_Enabled(t *testing.T) {
	resetHookState()

	ictx := hooktest.NewMockHookContext()
	AfterLogrusSetFormatter(ictx)

	std := logrus.StandardLogger()
	hasHook := false
	for _, hooks := range std.Hooks {
		for _, h := range hooks {
			if _, ok := h.(*traceHook); ok {
				hasHook = true
				break
			}
		}
	}
	assert.True(t, hasHook)
}

func TestAfterLogrusSetFormatter_Idempotent(t *testing.T) {
	resetHookState()

	ictx := hooktest.NewMockHookContext()
	AfterLogrusSetFormatter(ictx)

	std := logrus.StandardLogger()
	countAfterFirst := 0
	for _, hooks := range std.Hooks {
		for _, h := range hooks {
			if _, ok := h.(*traceHook); ok {
				countAfterFirst++
			}
		}
	}

	AfterLogrusSetFormatter(ictx)

	countAfterSecond := 0
	for _, hooks := range std.Hooks {
		for _, h := range hooks {
			if _, ok := h.(*traceHook); ok {
				countAfterSecond++
			}
		}
	}

	assert.Equal(t, countAfterFirst, countAfterSecond)
}
