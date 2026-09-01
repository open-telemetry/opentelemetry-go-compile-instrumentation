// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package zerolog

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
			enabledList: "logs/zerolog,logs/slog",
			expected:    true,
		},
		{
			name:        "not in enabled list",
			enabledList: "logs/slog",
			expected:    false,
		},
		{
			name:         "explicitly disabled",
			disabledList: "logs/zerolog",
			expected:     false,
		},
		{
			name:         "disabled takes precedence",
			enabledList:  "logs/zerolog",
			disabledList: "logs/zerolog",
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

			assert.Equal(t, tt.expected, logEnabler{}.Enable())
		})
	}
}

func TestTraceHook_NilEvent(t *testing.T) {
	traceHook{}.Run(nil, zerolog.InfoLevel, "")
}

func TestAfterZerologNew_Disabled(t *testing.T) {
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", instrumentationKey)
	ictx := hooktest.NewMockHookContext()
	AfterZerologNew(ictx, zerolog.New(nil))

	assert.Nil(t, ictx.GetReturnVal(0))
}

func TestAfterZerologNew_WithTraceContext(t *testing.T) {
	fields := runHookAndReadEvent(t, "trace-id", "span-id")

	assert.Equal(t, "trace-id", fields[traceIDKey])
	assert.Equal(t, "span-id", fields[spanIDKey])
}

func TestAfterZerologNew_TraceIDOnly(t *testing.T) {
	fields := runHookAndReadEvent(t, "trace-id", "")

	assert.Equal(t, "trace-id", fields[traceIDKey])
	assert.NotContains(t, fields, spanIDKey)
}

func TestAfterZerologNew_NoTraceContext(t *testing.T) {
	fields := runHookAndReadEvent(t, "", "")

	assert.NotContains(t, fields, traceIDKey)
	assert.NotContains(t, fields, spanIDKey)
}

func runHookAndReadEvent(t *testing.T, traceID, spanID string) map[string]interface{} {
	t.Helper()

	runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
		return traceID, spanID
	})
	t.Cleanup(func() {
		runtime.RegisterTraceAndSpanIDFunc(func() (string, string) {
			return "", ""
		})
	})

	var output bytes.Buffer
	ictx := hooktest.NewMockHookContext()
	AfterZerologNew(ictx, zerolog.New(&output))
	instrumented, ok := ictx.GetReturnVal(0).(zerolog.Logger)
	require.True(t, ok)
	instrumented.Info().Str("existing", "value").Msg("test message")

	var fields map[string]interface{}
	require.NoError(t, json.Unmarshal(output.Bytes(), &fields))
	assert.Equal(t, "value", fields["existing"])
	assert.Equal(t, "test message", fields["message"])
	return fields
}
