// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package slog

import (
	"testing"

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
			enabledList: "logs/slog,logs/log",
			expected:    true,
		},
		{
			name:        "not in enabled list",
			enabledList: "logs/log",
			expected:    false,
		},
		{
			name:         "explicitly disabled",
			disabledList: "logs/slog",
			expected:     false,
		},
		{
			name:         "enabled then disabled",
			enabledList:  "logs/slog,logs/log",
			disabledList: "logs/slog",
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

			// Create a new enabler to pick up the environment variables
			e := logEnabler{}
			result := e.Enable()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBeforeSlogLog_Disabled(t *testing.T) {
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "logs/slog")

	ictx := insttest.NewMockHookContext()
	logger := nil // logger is not used when disabled
	BeforeSlogLog(ictx, logger, nil, 0, "test message")
	// Should return early without modifying params
	assert.Nil(t, ictx.GetParam(4))
}

func TestBeforeSlogLog_EmptyMessage(t *testing.T) {
	ictx := insttest.NewMockHookContext()
	BeforeSlogLog(ictx, nil, nil, 0, "")
	// Should return early without modifying params
	assert.Nil(t, ictx.GetParam(4))
}

func TestBeforeSlogLog_AlreadyContainsTraceID(t *testing.T) {
	ictx := insttest.NewMockHookContext()
	BeforeSlogLog(ictx, nil, nil, 0, "message with trace_id=abc123")
	// Should return early without modifying params
	assert.Nil(t, ictx.GetParam(4))
}
