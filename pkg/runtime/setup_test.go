// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestShutdownSignals(t *testing.T) {
	signals := shutdownSignals()
	require.Len(t, signals, 2)
	assert.Contains(t, signals, os.Interrupt)
	assert.Contains(t, signals, syscall.SIGTERM)
}

func TestLogLevel(t *testing.T) {
	tests := []struct {
		envValue string
		expected slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"", slog.LevelInfo},        // unset defaults to info
		{"unknown", slog.LevelInfo}, // unrecognized value defaults to info
	}

	for _, tt := range tests {
		name := tt.envValue
		if name == "" {
			name = "unset"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("OTEL_LOG_LEVEL", tt.envValue)
			assert.Equal(t, tt.expected, logLevel())
		})
	}
}

func TestShutdownNilProviders(t *testing.T) {
	// The providers are package globals shared across tests, so save and restore
	// them to keep this test order-independent.
	origTracer, origMeter, origLogger := tracerProvider, meterProvider, loggerProvider
	t.Cleanup(func() { tracerProvider, meterProvider, loggerProvider = origTracer, origMeter, origLogger })

	// With no providers configured, Shutdown is a no-op that returns nil.
	tracerProvider = nil
	meterProvider = nil
	loggerProvider = nil
	require.NoError(t, Shutdown(context.Background()))
}

func TestShutdownProviders(t *testing.T) {
	origTracer, origMeter, origLogger := tracerProvider, meterProvider, loggerProvider
	t.Cleanup(func() { tracerProvider, meterProvider, loggerProvider = origTracer, origMeter, origLogger })

	// Providers with no exporters shut down cleanly, exercising both shutdown
	// branches without any network dependency.
	tracerProvider = sdktrace.NewTracerProvider()
	meterProvider = sdkmetric.NewMeterProvider()
	loggerProvider = sdklog.NewLoggerProvider()
	require.NoError(t, Shutdown(context.Background()))
}

// restoreProviders resets the global providers after a test that configures them.
func restoreProviders(t *testing.T) {
	origTracer, origMeter, origLogger := tracerProvider, meterProvider, loggerProvider
	t.Cleanup(func() { tracerProvider, meterProvider, loggerProvider = origTracer, origMeter, origLogger })
}

func TestSetupOpenTelemetry(t *testing.T) {
	// A configured OTLP endpoint drives the full trace-provider setup path.
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4317")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")
	restoreProviders(t)

	setupOpenTelemetry(Config{
		InstrumentationName:    "test-inst",
		InstrumentationVersion: "2.0.0",
	})
	assert.NotNil(t, tracerProvider, "trace provider should be configured when an OTLP endpoint is set")
}

func TestSetupOpenTelemetryPropagatorsFromEnv(t *testing.T) {
	// OTEL_PROPAGATORS must select the global propagator (here b3 single-header
	// instead of the default tracecontext+baggage).
	origProp := otel.GetTextMapPropagator()
	t.Cleanup(func() { otel.SetTextMapPropagator(origProp) })
	restoreProviders(t)

	// Disable all exporters so setup only exercises the propagator path.
	t.Setenv("OTEL_TRACES_EXPORTER", "none")
	t.Setenv("OTEL_METRICS_EXPORTER", "none")
	t.Setenv("OTEL_LOGS_EXPORTER", "none")
	t.Setenv("OTEL_PROPAGATORS", "b3")

	setupOpenTelemetry(Config{InstrumentationName: "test-inst"})

	fields := otel.GetTextMapPropagator().Fields()
	assert.Contains(t, fields, "x-b3-traceid",
		"OTEL_PROPAGATORS=b3 should install the b3 propagator")
	assert.NotContains(t, fields, "traceparent",
		"the default tracecontext propagator should be replaced")
}

func TestSetupOpenTelemetryPropagatorsInvalidValue(t *testing.T) {
	// An unrecognized OTEL_PROPAGATORS value falls back to the tracecontext+baggage
	// default rather than leaving the process without a propagator.
	origProp := otel.GetTextMapPropagator()
	t.Cleanup(func() { otel.SetTextMapPropagator(origProp) })
	restoreProviders(t)

	t.Setenv("OTEL_TRACES_EXPORTER", "none")
	t.Setenv("OTEL_METRICS_EXPORTER", "none")
	t.Setenv("OTEL_LOGS_EXPORTER", "none")
	t.Setenv("OTEL_PROPAGATORS", "not-a-propagator")

	setupOpenTelemetry(Config{InstrumentationName: "test-inst"})

	fields := otel.GetTextMapPropagator().Fields()
	assert.Contains(t, fields, "traceparent",
		"an unknown propagator name should fall back to the tracecontext default")
}

func TestSetupOpenTelemetryExporterError(t *testing.T) {
	// An invalid protocol makes the exporter fail to build, but setupOpenTelemetry
	// logs and swallows the error rather than propagating it or panicking.
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4317")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "invalid-protocol")
	restoreProviders(t)

	assert.NotPanics(t, func() {
		setupOpenTelemetry(Config{InstrumentationName: "test-service"})
	})
}

func TestSetupOpenTelemetryInstallsErrorHandler(t *testing.T) {
	// Errors the SDK reports through otel.Handle must reach the package logger
	// rather than the stdlib logger on stderr, which ignores OTEL_LOG_LEVEL.
	origHandler := otel.GetErrorHandler()
	t.Cleanup(func() { otel.SetErrorHandler(origHandler) })
	restoreProviders(t)

	t.Setenv("OTEL_TRACES_EXPORTER", "none")
	t.Setenv("OTEL_METRICS_EXPORTER", "none")
	t.Setenv("OTEL_LOGS_EXPORTER", "none")

	var buf bytes.Buffer
	origLogger := logger
	t.Cleanup(func() { logger = origLogger })
	logger = slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError}))

	setupOpenTelemetry(Config{InstrumentationName: "test-inst"})
	otel.Handle(errors.New("boom"))

	assert.Contains(t, buf.String(), "boom",
		"SDK errors should be routed through the package logger")
}

func TestInitializePanicRecovery(t *testing.T) {
	// Initialize must never crash the host application. A nil logger forces a
	// panic inside setup, which the deferred recover in Initialize must absorb.
	origLogger := logger
	t.Cleanup(func() {
		logger = origLogger
	})
	restoreProviders(t)

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4317")
	logger = nil

	assert.NotPanics(t, func() {
		Initialize(Config{InstrumentationName: "test-service"})
	})
}

func TestSetupOTelSDKDisabled(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		disabled bool
	}{
		{"literal true disables", "true", true},
		{"uppercase TRUE disables", "TRUE", true},
		{"mixed case True disables", "True", true},
		{"unset does not disable", "", false},
		{"false does not disable", "false", false},
		// Per spec, only the literal string "true" disables the SDK; other
		// truthy-looking values must not.
		{"arbitrary value does not disable", "1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("OTEL_SDK_DISABLED", tt.envValue)
			// Pin exporters to console so tests are hermetic regardless of the
			// runner's environment (e.g. OTEL_TRACES_EXPORTER=none).
			t.Setenv("OTEL_TRACES_EXPORTER", "console")
			t.Setenv("OTEL_METRICS_EXPORTER", "console")
			t.Setenv("OTEL_LOGS_EXPORTER", "console")
			restoreProviders(t)
			tracerProvider, meterProvider, loggerProvider = nil, nil, nil

			origTracerProvider := otel.GetTracerProvider()
			origMeterProvider := otel.GetMeterProvider()

			assert.NotPanics(t, func() {
				SetupOTelSDK()
			})

			if tt.disabled {
				assert.Nil(t, tracerProvider, "trace provider must not be installed when the SDK is disabled")
				assert.Nil(t, meterProvider, "meter provider must not be installed when the SDK is disabled")
				assert.Nil(t, loggerProvider, "logger provider must not be installed when the SDK is disabled")
				assert.Equal(t, origTracerProvider, otel.GetTracerProvider(),
					"global tracer provider must be untouched when the SDK is disabled")
				assert.Equal(t, origMeterProvider, otel.GetMeterProvider(),
					"global meter provider must be untouched when the SDK is disabled")
			} else {
				assert.NotNil(t, tracerProvider, "trace provider should be installed when the SDK is not disabled")
			}
		})
	}
}

func TestSetupTraceProviderConsoleExporterNoEndpoint(t *testing.T) {
	// Regression test: OTEL_TRACES_EXPORTER alone must decide the outcome.
	// Previously, setupTraceProvider silently skipped setup unless an OTLP
	// endpoint env var was also set, so console (and any other non-otlp
	// exporter) never produced a trace provider.
	t.Setenv("OTEL_TRACES_EXPORTER", "console")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	restoreProviders(t)
	tracerProvider = nil

	err := setupTraceProvider(context.Background(), resource.Default())

	require.NoError(t, err)
	assert.NotNil(t, tracerProvider,
		"trace provider should be initialized for OTEL_TRACES_EXPORTER=console even without an OTLP endpoint")
}

func TestSetupTraceProviderNoneExporter(t *testing.T) {
	t.Setenv("OTEL_TRACES_EXPORTER", "none")
	restoreProviders(t)
	tracerProvider = nil

	err := setupTraceProvider(context.Background(), resource.Default())

	require.NoError(t, err)
	assert.Nil(t, tracerProvider, "no trace provider should be installed for OTEL_TRACES_EXPORTER=none")
}

func TestSetupMeterProviderNoneExporter(t *testing.T) {
	// Metrics and traces must behave symmetrically for the "none" exporter.
	t.Setenv("OTEL_METRICS_EXPORTER", "none")
	restoreProviders(t)
	meterProvider = nil

	err := setupMeterProvider(context.Background(), resource.Default())

	require.NoError(t, err)
	assert.Nil(t, meterProvider, "no meter provider should be installed for OTEL_METRICS_EXPORTER=none")
}

func TestSetupLoggerProviderConsoleExporter(t *testing.T) {
	t.Setenv("OTEL_LOGS_EXPORTER", "console")
	restoreProviders(t)
	loggerProvider = nil

	err := setupLoggerProvider(context.Background(), resource.Default())

	require.NoError(t, err)
	assert.NotNil(t, loggerProvider,
		"logger provider should be initialized for OTEL_LOGS_EXPORTER=console even without an OTLP endpoint")
}

func TestSetupLoggerProviderNoneExporter(t *testing.T) {
	t.Setenv("OTEL_LOGS_EXPORTER", "none")
	restoreProviders(t)
	loggerProvider = nil

	err := setupLoggerProvider(context.Background(), resource.Default())

	require.NoError(t, err)
	assert.Nil(t, loggerProvider, "no logger provider should be installed for OTEL_LOGS_EXPORTER=none")
}
