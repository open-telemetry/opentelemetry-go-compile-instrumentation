// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package zap

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"

	"go.opentelemetry.io/otelc/pkg/hook/hooktest"
	"go.opentelemetry.io/otelc/pkg/runtime"
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
			enabledList: "logs/zap,logs/slog",
			expected:    true,
		},
		{
			name:        "not in enabled list",
			enabledList: "logs/slog",
			expected:    false,
		},
		{
			name:         "explicitly disabled",
			disabledList: "logs/zap",
			expected:     false,
		},
		{
			name:         "enabled then disabled",
			enabledList:  "logs/zap,logs/slog",
			disabledList: "logs/zap",
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

func TestBeforeCheckedEntryWrite_Disabled(t *testing.T) {
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "logs/zap")

	ictx := hooktest.NewMockHookContext()
	BeforeCheckedEntryWrite(ictx, &zapcore.CheckedEntry{})
	assert.Nil(t, ictx.GetParam(fieldsParamIndex))
}

func TestBeforeCheckedEntryWrite_NilEntry(t *testing.T) {
	ictx := hooktest.NewMockHookContext()
	BeforeCheckedEntryWrite(ictx, nil)
	assert.Nil(t, ictx.GetParam(fieldsParamIndex))
}

func TestBeforeCheckedEntryWrite_WithTraceContext(t *testing.T) {
	runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
		return "abc123traceId", "def456spanId"
	})
	t.Cleanup(func() {
		runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
			return "", ""
		})
	})

	ictx := hooktest.NewMockHookContext()
	BeforeCheckedEntryWrite(ictx, &zapcore.CheckedEntry{})

	got := requireFields(t, ictx)
	require.Len(t, got, 2)
	assert.Equal(t, stringField(traceIDKey, "abc123traceId"), got[0])
	assert.Equal(t, stringField(spanIDKey, "def456spanId"), got[1])
}

func TestBeforeCheckedEntryWrite_WithTraceIDOnly(t *testing.T) {
	runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
		return "abc123traceId", ""
	})
	t.Cleanup(func() {
		runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
			return "", ""
		})
	})

	ictx := hooktest.NewMockHookContext()
	BeforeCheckedEntryWrite(ictx, &zapcore.CheckedEntry{})

	got := requireFields(t, ictx)
	require.Len(t, got, 1)
	assert.Equal(t, stringField(traceIDKey, "abc123traceId"), got[0])
}

func TestBeforeCheckedEntryWrite_NoTraceContext(t *testing.T) {
	runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
		return "", ""
	})

	ictx := hooktest.NewMockHookContext()
	BeforeCheckedEntryWrite(ictx, &zapcore.CheckedEntry{})
	assert.Nil(t, ictx.GetParam(fieldsParamIndex))
}

func TestBeforeCheckedEntryWrite_PreservesExistingFields(t *testing.T) {
	runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
		return "abc123traceId", "def456spanId"
	})
	t.Cleanup(func() {
		runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
			return "", ""
		})
	})

	existing := stringField("key", "value")
	ictx := hooktest.NewMockHookContext()
	BeforeCheckedEntryWrite(ictx, &zapcore.CheckedEntry{}, existing)

	got := requireFields(t, ictx)
	require.Len(t, got, 3)
	assert.Equal(t, existing, got[0])
	assert.Equal(t, stringField(traceIDKey, "abc123traceId"), got[1])
	assert.Equal(t, stringField(spanIDKey, "def456spanId"), got[2])
}

func TestBeforeCheckedEntryWrite_DoesNotAliasCallerSlice(t *testing.T) {
	runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
		return "abc123traceId", "def456spanId"
	})
	t.Cleanup(func() {
		runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
			return "", ""
		})
	})

	caller := make([]zapcore.Field, 1, 4)
	caller[0] = stringField("key", "value")

	ictx := hooktest.NewMockHookContext()
	BeforeCheckedEntryWrite(ictx, &zapcore.CheckedEntry{}, caller...)

	require.Equal(t, []zapcore.Field{stringField("key", "value")}, caller)
	assert.Equal(t, zapcore.Field{}, caller[:cap(caller)][1])
	assert.Equal(t, zapcore.Field{}, caller[:cap(caller)][2])

	got := requireFields(t, ictx)
	require.Len(t, got, 3)
	assert.Equal(t, stringField("key", "value"), got[0])
	assert.Equal(t, stringField(traceIDKey, "abc123traceId"), got[1])
	assert.Equal(t, stringField(spanIDKey, "def456spanId"), got[2])
}

func requireFields(t *testing.T, ictx *hooktest.MockHookContext) []zapcore.Field {
	t.Helper()
	raw := ictx.GetParam(fieldsParamIndex)
	require.NotNil(t, raw)
	got, ok := raw.([]zapcore.Field)
	require.True(t, ok)
	return got
}
