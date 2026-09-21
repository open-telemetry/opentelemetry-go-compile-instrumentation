// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package log

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

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

func TestBeforeLogOutput_WithTraceContext(t *testing.T) {
	runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
		return "abc123traceId", "def456spanId"
	})
	defer runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
		return "", ""
	})

	ictx := hooktest.NewMockHookContext()
	originalAppend := func(b []byte) []byte { return append(b, []byte("hello world\n")...) }
	BeforeLogOutput(ictx, nil, 0, 0, originalAppend)

	wrappedFn := ictx.GetParam(3)
	assert.NotNil(t, wrappedFn)
	wrapped := wrappedFn.(func([]byte) []byte)
	result := string(wrapped([]byte{}))
	assert.Contains(t, result, "hello world")
	assert.Contains(t, result, "trace_id=abc123traceId")
	assert.Contains(t, result, "span_id=def456spanId")
}

func TestBeforeLogOutput_WithTraceIDOnly(t *testing.T) {
	runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
		return "abc123traceId", ""
	})
	defer runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
		return "", ""
	})

	ictx := hooktest.NewMockHookContext()
	originalAppend := func(b []byte) []byte { return append(b, []byte("hello\n")...) }
	BeforeLogOutput(ictx, nil, 0, 0, originalAppend)

	wrappedFn := ictx.GetParam(3)
	wrapped := wrappedFn.(func([]byte) []byte)
	result := string(wrapped([]byte{}))
	assert.Contains(t, result, "trace_id=abc123traceId")
	assert.NotContains(t, result, "span_id=")
}

func TestBeforeLogOutput_NoTraceContext(t *testing.T) {
	runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
		return "", ""
	})

	ictx := hooktest.NewMockHookContext()
	originalAppend := func(b []byte) []byte { return append(b, []byte("hello\n")...) }
	BeforeLogOutput(ictx, nil, 0, 0, originalAppend)

	wrappedFn := ictx.GetParam(3)
	wrapped := wrappedFn.(func([]byte) []byte)
	result := string(wrapped([]byte{}))
	assert.Equal(t, "hello\n", result)
	assert.NotContains(t, result, "trace_id=")
}

func TestBeforeLogOutput_AlreadyContainsTraceID(t *testing.T) {
	runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
		return "abc123", "def456"
	})
	defer runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
		return "", ""
	})

	ictx := hooktest.NewMockHookContext()
	originalAppend := func(b []byte) []byte {
		return append(b, []byte("msg trace_id=existing\n")...)
	}
	BeforeLogOutput(ictx, nil, 0, 0, originalAppend)

	wrappedFn := ictx.GetParam(3)
	wrapped := wrappedFn.(func([]byte) []byte)
	result := string(wrapped([]byte{}))
	assert.Contains(t, result, "trace_id=existing")
	assert.NotContains(t, result, "trace_id=abc123")
}

func TestBeforeLogOutput_TraceIDMention(t *testing.T) {
	runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
		return "abc123", "def456"
	})
	t.Cleanup(func() {
		runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
			return "", ""
		})
	})

	ictx := hooktest.NewMockHookContext()
	originalAppend := func(b []byte) []byte {
		return append(b, []byte("could not parse trace_id header\n")...)
	}
	BeforeLogOutput(ictx, nil, 0, 0, originalAppend)

	wrappedFn := ictx.GetParam(3)
	wrapped := wrappedFn.(func([]byte) []byte)
	result := string(wrapped([]byte{}))
	assert.Contains(t, result, "could not parse trace_id header")
	assert.Contains(t, result, "trace_id=abc123")
	assert.Contains(t, result, "span_id=def456")
}

func TestBeforeLogOutput_OtherTraceIDPrefix(t *testing.T) {
	runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
		return "abc123", "def456"
	})
	t.Cleanup(func() {
		runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
			return "", ""
		})
	})

	ictx := hooktest.NewMockHookContext()
	originalAppend := func(b []byte) []byte {
		return append(b, []byte("msg other_trace_id=custom123\n")...)
	}
	BeforeLogOutput(ictx, nil, 0, 0, originalAppend)

	wrappedFn := ictx.GetParam(3)
	wrapped := wrappedFn.(func([]byte) []byte)
	result := string(wrapped([]byte{}))
	assert.Contains(t, result, "other_trace_id=custom123")
	assert.Contains(t, result, "trace_id=abc123")
	assert.Contains(t, result, "span_id=def456")
}

func TestBeforeLogOutput_BracketedTraceID(t *testing.T) {
	runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
		return "abc123", "def456"
	})
	t.Cleanup(func() {
		runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
			return "", ""
		})
	})

	ictx := hooktest.NewMockHookContext()
	originalAppend := func(b []byte) []byte {
		return append(b, []byte("[trace_id=existing] user logged in\n")...)
	}
	BeforeLogOutput(ictx, nil, 0, 0, originalAppend)

	wrappedFn := ictx.GetParam(3)
	wrapped := wrappedFn.(func([]byte) []byte)
	result := string(wrapped([]byte{}))
	assert.Contains(t, result, "[trace_id=existing]")
	assert.Contains(t, result, "trace_id=abc123")
	assert.Contains(t, result, "span_id=def456")
}

func TestBeforeLogOutput_LeadingTraceID(t *testing.T) {
	runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
		return "abc123", "def456"
	})
	t.Cleanup(func() {
		runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
			return "", ""
		})
	})

	ictx := hooktest.NewMockHookContext()
	originalAppend := func(b []byte) []byte {
		return append(b, []byte("trace_id=existing message\n")...)
	}
	BeforeLogOutput(ictx, nil, 0, 0, originalAppend)

	wrappedFn := ictx.GetParam(3)
	wrapped := wrappedFn.(func([]byte) []byte)
	result := string(wrapped([]byte{}))
	assert.Contains(t, result, "trace_id=existing message")
	assert.Contains(t, result, "trace_id=abc123")
	assert.Contains(t, result, "span_id=def456")
}

func TestBeforeLogOutput_EmptyOutput(t *testing.T) {
	runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
		return "abc123", "def456"
	})
	defer runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
		return "", ""
	})

	ictx := hooktest.NewMockHookContext()
	originalAppend := func(b []byte) []byte { return b }
	BeforeLogOutput(ictx, nil, 0, 0, originalAppend)

	wrappedFn := ictx.GetParam(3)
	wrapped := wrappedFn.(func([]byte) []byte)
	result := wrapped([]byte{})
	assert.Empty(t, result)
}

func TestBeforeLogOutput_PreservesLineEnding(t *testing.T) {
	tests := []struct {
		name        string
		lineEnding  string
		wantContain string
	}{
		{
			name:        "LF",
			lineEnding:  "\n",
			wantContain: "msg trace_id=abc123 span_id=def456\n",
		},
		{
			name:        "CRLF",
			lineEnding:  "\r\n",
			wantContain: "msg trace_id=abc123 span_id=def456\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
				return "abc123", "def456"
			})
			t.Cleanup(func() {
				runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
					return "", ""
				})
			})

			ictx := hooktest.NewMockHookContext()
			originalAppend := func(b []byte) []byte { return append(b, []byte("msg"+tt.lineEnding)...) }
			BeforeLogOutput(ictx, nil, 0, 0, originalAppend)

			wrappedFn := ictx.GetParam(3)
			wrapped := wrappedFn.(func([]byte) []byte)
			result := string(wrapped([]byte{}))
			assert.True(t, strings.HasSuffix(result, tt.lineEnding))
			assert.Contains(t, result, tt.wantContain)
		})
	}
}

func TestHasTraceID_ZeroAllocs(t *testing.T) {
	msg := []byte("2026/09/18 10:00:00 standard log message without trace id\n")
	allocs := testing.AllocsPerRun(1000, func() {
		_ = hasTraceID(msg)
	})
	assert.Equal(t, float64(0), allocs)

	matchMsg := []byte("2026/09/18 10:00:00 standard log message trace_id=abcdef123456\n")
	matchAllocs := testing.AllocsPerRun(1000, func() {
		_ = hasTraceID(matchMsg)
	})
	assert.Equal(t, float64(0), matchAllocs)
}

func BenchmarkHasTraceID_NoMatch(b *testing.B) {
	msg := []byte("2026/09/18 10:00:00 standard log message without trace id\n")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = hasTraceID(msg)
	}
}

func BenchmarkHasTraceID_Match(b *testing.B) {
	msg := []byte("2026/09/18 10:00:00 standard log message trace_id=abcdef123456\n")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = hasTraceID(msg)
	}
}
