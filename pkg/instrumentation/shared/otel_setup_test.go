// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package shared

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetLogger(t *testing.T) {
	logger1 := GetLogger()
	require.NotNil(t, logger1)

	// Should return the same instance (singleton)
	logger2 := GetLogger()
	assert.Equal(t, logger1, logger2)
}

func TestSetupOTelSDK(t *testing.T) {
	err := SetupOTelSDK()
	require.NoError(t, err)

	// Should be idempotent
	err = SetupOTelSDK()
	require.NoError(t, err)
}

func TestInstrumented(t *testing.T) {
	tests := []struct {
		name                string
		globalEnv           string
		specificEnv         string
		instrumentationName string
		expected            bool
	}{
		{
			name:                "default enabled",
			globalEnv:           "",
			specificEnv:         "",
			instrumentationName: "NETHTTP",
			expected:            true,
		},
		{
			name:                "globally disabled",
			globalEnv:           "false",
			specificEnv:         "",
			instrumentationName: "NETHTTP",
			expected:            false,
		},
		{
			name:                "specifically disabled",
			globalEnv:           "",
			specificEnv:         "false",
			instrumentationName: "NETHTTP",
			expected:            false,
		},
		{
			name:                "specifically enabled overrides nothing",
			globalEnv:           "",
			specificEnv:         "true",
			instrumentationName: "NETHTTP",
			expected:            true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.globalEnv != "" {
				t.Setenv("OTEL_INSTRUMENTATION_ENABLED", tt.globalEnv)
			}
			if tt.specificEnv != "" {
				envVar := "OTEL_INSTRUMENTATION_" + tt.instrumentationName + "_ENABLED"
				t.Setenv(envVar, tt.specificEnv)
			}

			result := Instrumented(tt.instrumentationName)
			assert.Equal(t, tt.expected, result)
		})
	}
}
