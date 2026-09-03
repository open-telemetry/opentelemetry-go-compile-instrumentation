// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetLogger(t *testing.T) {
	logger1 := Logger()
	require.NotNil(t, logger1)

	// Should return the same instance (singleton)
	logger2 := Logger()
	assert.Equal(t, logger1, logger2)
}

func TestInstrumented(t *testing.T) {
	tests := []struct {
		name                string
		enabledList         string
		disabledList        string
		instrumentationName string
		expected            bool
	}{
		{
			name:                "default enabled",
			enabledList:         "",
			disabledList:        "",
			instrumentationName: "nethttp",
			expected:            true,
		},
		{
			name:                "explicitly enabled",
			enabledList:         "nethttp,grpc",
			disabledList:        "",
			instrumentationName: "nethttp",
			expected:            true,
		},
		{
			name:                "not in enabled list",
			enabledList:         "grpc",
			disabledList:        "",
			instrumentationName: "nethttp",
			expected:            false,
		},
		{
			name:                "explicitly disabled",
			enabledList:         "",
			disabledList:        "nethttp",
			instrumentationName: "nethttp",
			expected:            false,
		},
		{
			name:                "enabled then disabled",
			enabledList:         "nethttp,grpc",
			disabledList:        "nethttp",
			instrumentationName: "nethttp",
			expected:            false,
		},
		{
			name:                "case insensitive",
			enabledList:         "NETHTTP,GRPC",
			disabledList:        "",
			instrumentationName: "NetHTTP",
			expected:            true,
		},
		{
			name:                "with spaces",
			enabledList:         " nethttp , grpc ",
			disabledList:        "",
			instrumentationName: "nethttp",
			expected:            true,
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

			result := Instrumented(tt.instrumentationName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestInstrumented_OTelSDKDisabled(t *testing.T) {
	tests := []struct {
		value    string
		expected bool // expected value of Instrumented() (false when disabled, true when enabled)
	}{
		// Mixed-case true (should disable)
		{"true", false},
		{"TRUE", false},
		{"True", false},
		{"tRue", false},

		// Surrounding whitespace (should NOT disable, consistent with SetupOTelSDK and OTel spec)
		{" true ", true},
		{"true ", true},
		{" true", true},

		// Invalid/other values (should NOT disable)
		{"false", true},
		{"1", true},
		{"yes", true},
		{"0", true},
		{"invalid", true},
	}

	for _, tt := range tests {
		t.Run("OTEL_SDK_DISABLED="+tt.value, func(t *testing.T) {
			t.Setenv("OTEL_SDK_DISABLED", tt.value)
			assert.Equal(t, tt.expected, Instrumented("nethttp"))
			assert.Equal(t, tt.expected, Instrumented("grpc"))
		})
	}
}

func BenchmarkInstrumented(b *testing.B) {
	b.Run("unset", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = Instrumented("nethttp")
		}
	})

	b.Run("disabled-list", func(b *testing.B) {
		b.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "grpc,gin")
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = Instrumented("nethttp")
		}
	})

	b.Run("configured-list", func(b *testing.B) {
		b.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp,grpc,gin,redis,mongodb,kafka")
		b.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "grpc,gin")
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = Instrumented("nethttp")
		}
	})
}
