// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package logrus

import (
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/hook/hooktest"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/runtime"
)

func resetHookState() {
	hookInitMu.Lock()
	defer hookInitMu.Unlock()
	hookInitMap = make(map[*logrus.Logger]bool)
	fieldInitMap = make(map[*logrus.Logger]bool)
	formatterInit = false
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
