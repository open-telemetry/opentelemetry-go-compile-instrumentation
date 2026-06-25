// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
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

func TestLogLevel(t *testing.T) {
	tests := []struct {
		envVal   string
		expected slog.Level
	}{
		{envVal: "debug", expected: slog.LevelDebug},
		{envVal: "info", expected: slog.LevelInfo},
		{envVal: "warn", expected: slog.LevelWarn},
		{envVal: "error", expected: slog.LevelError},
		{envVal: "", expected: slog.LevelInfo},
		{envVal: "unknown", expected: slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run("OTEL_LOG_LEVEL="+tt.envVal, func(t *testing.T) {
			t.Setenv("OTEL_LOG_LEVEL", tt.envVal)
			assert.Equal(t, tt.expected, logLevel())
		})
	}
}

func TestInitialize(t *testing.T) {
	// Initialize is executed with sync.Once, so calling it here is safe.
	cfg := Config{
		ServiceName:            "test-service",
		ServiceVersion:         "1.0.0",
		InstrumentationName:    "test-inst",
		InstrumentationVersion: "2.0.0",
	}

	// Should not panic
	assert.NotPanics(t, func() {
		Initialize(cfg)
	})
}

func TestShutdown(t *testing.T) {
	ctx := context.Background()

	// If both are nil, Shutdown should return nil
	tracerProvider = nil
	meterProvider = nil
	err := Shutdown(ctx)
	assert.NoError(t, err)

	// Set them to some instances
	tracerProvider = sdktrace.NewTracerProvider()
	meterProvider = sdkmetric.NewMeterProvider()

	err = Shutdown(ctx)
	assert.NoError(t, err)

	// Clean up
	tracerProvider = nil
	meterProvider = nil
}

func TestSetupOpenTelemetry(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4317")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")

	cfg := Config{
		ServiceName:            "test-service-opentelemetry",
		ServiceVersion:         "1.0.0",
		InstrumentationName:    "test-inst",
		InstrumentationVersion: "2.0.0",
	}

	err := setupOpenTelemetry(cfg)
	assert.NoError(t, err)

	// Clean up global providers so they don't affect other tests
	t.Cleanup(func() {
		tracerProvider = nil
		meterProvider = nil
		otel.SetTracerProvider(otel.GetTracerProvider())
		otel.SetMeterProvider(otel.GetMeterProvider())
	})
}

func TestSetupOpenTelemetry_Errors(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4317")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "invalid-protocol")

	cfg := Config{
		ServiceName:            "test-service-errors",
		ServiceVersion:         "1.0.0",
		InstrumentationName:    "test-inst",
		InstrumentationVersion: "2.0.0",
	}

	err := setupOpenTelemetry(cfg)
	assert.NoError(t, err) // setupOpenTelemetry swallows exporter errors and returns nil

	t.Cleanup(func() {
		tracerProvider = nil
		meterProvider = nil
		otel.SetTracerProvider(otel.GetTracerProvider())
		otel.SetMeterProvider(otel.GetMeterProvider())
	})
}

func TestInitialize_PanicRecovery(t *testing.T) {
	// Save the original logger and restore at end
	origLogger := logger
	defer func() {
		logger = origLogger
		initOnce = sync.Once{}
	}()

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4317")

	cfg := Config{
		ServiceName:            "test-service-panic",
		ServiceVersion:         "1.0.0",
		InstrumentationName:    "test-inst",
		InstrumentationVersion: "2.0.0",
	}

	// Trigger panic by setting logger to nil
	logger = nil
	initOnce = sync.Once{}

	assert.NotPanics(t, func() {
		Initialize(cfg)
	})
}

