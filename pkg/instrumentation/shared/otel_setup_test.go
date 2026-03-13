// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package shared

import (
	"sync"
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

func TestSetupOTelSDK(t *testing.T) {
	var (
		instrumentationName    = "github.com/open-telemetry/opentelemetry-go-compile-instrumentation"
		instrumentationVersion = "0.1.0"
	)
	err := SetupOTelSDK(instrumentationName, instrumentationVersion)
	require.NoError(t, err)

	// Should be idempotent
	err = SetupOTelSDK(instrumentationName, instrumentationVersion)
	require.NoError(t, err)
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

// TestStartRuntimeMetrics_Idempotent verifies that StartRuntimeMetrics can be
// called multiple times without panicking and that subsequent calls return the
// same error value as the first call (sync.OnceValue semantics: the underlying
// start attempt runs exactly once and the result is cached).
func TestStartRuntimeMetrics_Idempotent(t *testing.T) {
	// Disable runtime metrics so the test does not depend on a real meter
	// provider being configured. Instrumented("runtimemetrics") will return
	// false, causing startRuntimeMetrics to return nil on the first call.
	// All subsequent calls must return the same cached nil.
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "runtimemetrics")

	err1 := StartRuntimeMetrics()
	require.NoError(t, err1, "first call should succeed when runtime metrics are disabled")

	err2 := StartRuntimeMetrics()
	assert.Equal(t, err1, err2, "second call must return the same cached error as the first")

	// Call from multiple goroutines to verify no data race or panic under
	// concurrent access. sync.OnceValue guarantees a single execution, but
	// we exercise the concurrent path explicitly.
	const goroutines = 10
	results := make([]error, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(idx int) {
			defer wg.Done()
			results[idx] = StartRuntimeMetrics()
		}(i)
	}
	wg.Wait()

	for i, err := range results {
		assert.Equal(t, err1, err, "concurrent call %d must return the same cached error", i)
	}
}
