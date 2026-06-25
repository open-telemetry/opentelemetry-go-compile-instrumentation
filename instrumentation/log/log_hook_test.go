// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package log

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/hook/hooktest"
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

	ictx := hooktest.NewMockHookContext()
	appendOutput := func(b []byte) []byte { return b }
	BeforeLogOutput(ictx, nil, 0, 0, appendOutput)
	assert.Nil(t, ictx.GetParam(3))
}

func TestBeforeLogOutput_WrapsAppendOutput(t *testing.T) {
	ictx := hooktest.NewMockHookContext()
	originalAppend := func(b []byte) []byte { return append(b, []byte("original")...) }
	BeforeLogOutput(ictx, nil, 0, 0, originalAppend)

	wrappedFn := ictx.GetParam(3)
	assert.NotNil(t, wrappedFn)
	wrapped := wrappedFn.(func([]byte) []byte)
	result := wrapped([]byte{})
	assert.Contains(t, string(result), "original")
}
