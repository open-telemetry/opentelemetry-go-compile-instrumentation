// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package slog

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/hook/hooktest"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/runtime"
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

			e := logEnabler{}
			result := e.Enable()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAfterSlogNewRecord_Disabled(t *testing.T) {
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "logs/slog")

	ictx := hooktest.NewMockHookContext()
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "test", 0)
	AfterSlogNewRecord(ictx, r)
	assert.Nil(t, ictx.GetReturnVal(0))
}

func TestAfterSlogNewRecord_WithTraceContext(t *testing.T) {
	runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
		return "abc123traceId", "def456spanId"
	})
	defer runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
		return "", ""
	})

	ictx := hooktest.NewMockHookContext()
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "test message", 0)
	AfterSlogNewRecord(ictx, r)

	result := ictx.GetReturnVal(0)
	assert.NotNil(t, result)
	record := result.(slog.Record)

	var attrs []slog.Attr
	record.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})

	assert.Len(t, attrs, 2)
	assert.Equal(t, "trace_id", attrs[0].Key)
	assert.Equal(t, "abc123traceId", attrs[0].Value.String())
	assert.Equal(t, "span_id", attrs[1].Key)
	assert.Equal(t, "def456spanId", attrs[1].Value.String())
}

func TestAfterSlogNewRecord_WithTraceIDOnly(t *testing.T) {
	runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
		return "abc123traceId", ""
	})
	defer runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
		return "", ""
	})

	ictx := hooktest.NewMockHookContext()
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "test message", 0)
	AfterSlogNewRecord(ictx, r)

	result := ictx.GetReturnVal(0)
	assert.NotNil(t, result)
	record := result.(slog.Record)

	var attrs []slog.Attr
	record.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})

	assert.Len(t, attrs, 1)
	assert.Equal(t, "trace_id", attrs[0].Key)
	assert.Equal(t, "abc123traceId", attrs[0].Value.String())
}

func TestAfterSlogNewRecord_NoTraceContext(t *testing.T) {
	runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
		return "", ""
	})

	ictx := hooktest.NewMockHookContext()
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "test message", 0)
	AfterSlogNewRecord(ictx, r)
	assert.Nil(t, ictx.GetReturnVal(0))
}
