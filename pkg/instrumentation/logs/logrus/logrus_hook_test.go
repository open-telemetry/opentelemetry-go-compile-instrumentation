// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package logrus

import (
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst/insttest"
)

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

func TestAfterLogrusNew_Disabled(t *testing.T) {
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "logs/logrus")

	ictx := insttest.NewMockHookContext()
	logger := logrus.New()
	AfterLogrusNew(ictx, logger)
	// Should return early without adding hook
	assert.Empty(t, logger.Hooks)
}

func TestAfterLogrusNew_NilLogger(t *testing.T) {
	ictx := insttest.NewMockHookContext()
	AfterLogrusNew(ictx, nil)
	// Should return early without panic
}

func TestAfterLogrusWithField_Disabled(t *testing.T) {
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "logs/logrus")

	ictx := insttest.NewMockHookContext()
	logger := logrus.New()
	entry := &logrus.Entry{Logger: logger}
	AfterLogrusWithField(ictx, entry)
	// Should return early without adding hook
	assert.Empty(t, logger.Hooks)
}

func TestBeforeLogrusEntryLog_Disabled(t *testing.T) {
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "logs/logrus")

	ictx := insttest.NewMockHookContext()
	entry := &logrus.Entry{}
	BeforeLogrusEntryLog(ictx, entry, logrus.InfoLevel, "test")
	// Should return early without modifying params
	assert.Nil(t, ictx.GetParam(2))
}

func TestBeforeLogrusEntryLog_NilArgs(t *testing.T) {
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "logs/logrus")

	ictx := insttest.NewMockHookContext()
	entry := &logrus.Entry{}
	BeforeLogrusEntryLog(ictx, entry, logrus.InfoLevel)
	// Should return early without modifying params
	assert.Nil(t, ictx.GetParam(2))
}
