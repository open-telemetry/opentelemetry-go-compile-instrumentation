// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package log

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
			enabledList: "logs/log,logs/slog",
			expected:    true,
		},
		{
			name:        "not in enabled list",
			enabledList: "logs/slog",
			expected:    false,
		},
		{
			name:         "explicitly disabled",
			disabledList: "logs/log",
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

func TestBeforeLogOutput_Disabled(t *testing.T) {
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "logs/log")

	ictx := insttest.NewMockHookContext()
	appendOutput := func(b []byte) []byte { return b }
	BeforeLogOutput(ictx, nil, 0, 0, appendOutput)
	// Should return early without modifying params
	assert.Equal(t, appendOutput, ictx.GetParam(3))
}

func TestBeforeLogOutput_WrapsAppendOutput(t *testing.T) {
	ictx := insttest.NewMockHookContext()
	originalAppend := func(b []byte) []byte { return append(b, []byte("original")...) }
	BeforeLogOutput(ictx, nil, 0, 0, originalAppend)

	// Should wrap the appendOutput function
	wrapped, ok := ictx.GetParam(3).(func([]byte) []byte)
	assert.True(t, ok, "param 3 should be a function")
	assert.NotNil(t, wrapped)

	// Test that the wrapped function modifies output
	result := wrapped([]byte("test "))
	assert.Contains(t, string(result), "original")
}
