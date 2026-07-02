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
