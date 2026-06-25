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

func TestBeforeSlogLog_Disabled(t *testing.T) {
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "logs/slog")

	ictx := hooktest.NewMockHookContext()
	BeforeSlogLog(ictx, nil, nil, 0, "test message")
	assert.Nil(t, ictx.GetParam(4))
}

func TestBeforeSlogLog_EmptyMessage(t *testing.T) {
	ictx := hooktest.NewMockHookContext()
	BeforeSlogLog(ictx, nil, nil, 0, "")
	assert.Nil(t, ictx.GetParam(4))
}

func TestBeforeSlogLog_AlreadyContainsTraceID(t *testing.T) {
	runtime.RegisterTraceAndSpanIdFunc(func() (string, string) {
		return "abc123", "def456"
	})
	defer runtime.RegisterTraceAndSpanIdFunc(func() (string, string) {
		return "", ""
	})

	ictx := hooktest.NewMockHookContext()
	BeforeSlogLog(ictx, nil, nil, 0, "message with trace_id=abc123")
	assert.Nil(t, ictx.GetParam(4))
}

func TestBeforeSlogLog_WithTraceContext(t *testing.T) {
	runtime.RegisterTraceAndSpanIdFunc(func() (string, string) {
		return "abc123traceId", "def456spanId"
	})
	defer runtime.RegisterTraceAndSpanIdFunc(func() (string, string) {
		return "", ""
	})

	ictx := hooktest.NewMockHookContext()
	BeforeSlogLog(ictx, nil, nil, 0, "hello world")

	result := ictx.GetParam(4)
	assert.NotNil(t, result)
	msg := result.(string)
	assert.Contains(t, msg, "hello world")
	assert.Contains(t, msg, "trace_id=abc123traceId")
	assert.Contains(t, msg, "span_id=def456spanId")
}

func TestBeforeSlogLog_WithTraceIdOnly(t *testing.T) {
	runtime.RegisterTraceAndSpanIdFunc(func() (string, string) {
		return "abc123traceId", ""
	})
	defer runtime.RegisterTraceAndSpanIdFunc(func() (string, string) {
		return "", ""
	})

	ictx := hooktest.NewMockHookContext()
	BeforeSlogLog(ictx, nil, nil, 0, "hello world")

	result := ictx.GetParam(4)
	assert.NotNil(t, result)
	msg := result.(string)
	assert.Contains(t, msg, "trace_id=abc123traceId")
	assert.NotContains(t, msg, "span_id=")
}

func TestBeforeSlogLog_NoTraceContext(t *testing.T) {
	runtime.RegisterTraceAndSpanIdFunc(func() (string, string) {
		return "", ""
	})

	ictx := hooktest.NewMockHookContext()
	BeforeSlogLog(ictx, nil, nil, 0, "hello world")
	assert.Nil(t, ictx.GetParam(4))
}

func TestAfterSlogNewRecord_Disabled(t *testing.T) {
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "logs/slog")

	ictx := hooktest.NewMockHookContext()
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "test", 0)
	AfterSlogNewRecord(ictx, r)
	assert.Nil(t, ictx.GetReturnVal(0))
}

func TestAfterSlogNewRecord_WithTraceContext(t *testing.T) {
	runtime.RegisterTraceAndSpanIdFunc(func() (string, string) {
		return "abc123traceId", "def456spanId"
	})
	defer runtime.RegisterTraceAndSpanIdFunc(func() (string, string) {
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

func TestAfterSlogNewRecord_WithTraceIdOnly(t *testing.T) {
	runtime.RegisterTraceAndSpanIdFunc(func() (string, string) {
		return "abc123traceId", ""
	})
	defer runtime.RegisterTraceAndSpanIdFunc(func() (string, string) {
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
	runtime.RegisterTraceAndSpanIdFunc(func() (string, string) {
		return "", ""
	})

	ictx := hooktest.NewMockHookContext()
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "test message", 0)
	AfterSlogNewRecord(ictx, r)
	assert.Nil(t, ictx.GetReturnVal(0))
}
