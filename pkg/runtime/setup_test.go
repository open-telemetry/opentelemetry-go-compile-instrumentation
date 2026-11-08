// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	stdruntime "runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestShutdownSignals(t *testing.T) {
	signals := shutdownSignals()
	require.Len(t, signals, 2)
	assert.Contains(t, signals, os.Interrupt)
	assert.Contains(t, signals, syscall.SIGTERM)
}

// errShutdownProcessor is a span processor whose Shutdown always fails, used to
// exercise the handler's error-logging branch.
type errShutdownProcessor struct{}

func (errShutdownProcessor) OnStart(context.Context, sdktrace.ReadWriteSpan) {}
func (errShutdownProcessor) OnEnd(sdktrace.ReadOnlySpan)                     {}
func (errShutdownProcessor) ForceFlush(context.Context) error                { return nil }
func (errShutdownProcessor) Shutdown(context.Context) error                  { return errors.New("shutdown failed") }

// recordingProcessor records whether Shutdown was called.
type recordingProcessor struct{ shutdownCalled bool }

func (*recordingProcessor) OnStart(context.Context, sdktrace.ReadWriteSpan) {}
func (*recordingProcessor) OnEnd(sdktrace.ReadOnlySpan)                     {}
func (*recordingProcessor) ForceFlush(context.Context) error                { return nil }
func (p *recordingProcessor) Shutdown(context.Context) error                { p.shutdownCalled = true; return nil }

// runShutdownHandler drives handleShutdownSignal inline with a SIGTERM already
// queued, exercising the flush path without delivering a real signal to the test
// process. The os/signal instrumentation is not applied to unit tests, so the
// handler does not re-raise, and reaching the return proves it does not exit.
func runShutdownHandler(t *testing.T) {
	t.Helper()
	sigCh := make(chan os.Signal, 1)
	sigCh <- syscall.SIGTERM
	handleShutdownSignal(sigCh)
}

func TestHandleShutdownSignalFlushesProviders(t *testing.T) {
	restoreProviders(t)
	rp := &recordingProcessor{}
	tracerProvider = sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rp))
	meterProvider, loggerProvider = nil, nil

	runShutdownHandler(t)

	assert.True(t, rp.shutdownCalled, "handler should flush the tracer provider on shutdown")
}

func TestHandleShutdownSignalLogsFlushError(t *testing.T) {
	restoreProviders(t)
	tracerProvider = sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(errShutdownProcessor{}))
	meterProvider, loggerProvider = nil, nil

	var buf bytes.Buffer
	origLogger := logger
	t.Cleanup(func() { logger = origLogger })
	logger = slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError}))

	runShutdownHandler(t)

	assert.Contains(t, buf.String(), "shutdown failed",
		"flush errors should be logged during shutdown")
}

// resetSignalOwners clears the signal.Notify record before and after the test.
// Unit tests are not instrumented, so the record is empty outside such tests.
func resetSignalOwners(t *testing.T) {
	t.Helper()
	reset := func() {
		signalOwners.Lock()
		defer signalOwners.Unlock()
		signalOwners.otelcCh = nil
		signalOwners.hookInstalled = false
		signalOwners.allSignals = false
		signalOwners.signals = nil
	}
	reset()
	t.Cleanup(reset)
}

func TestShouldReraise(t *testing.T) {
	tests := []struct {
		name        string
		hook        bool
		appSignals  [][]os.Signal // one entry per application signal.Notify call
		wantSIGTERM bool
		wantSIGINT  bool
	}{
		{name: "hook not applied", hook: false, wantSIGTERM: false, wantSIGINT: false},
		{name: "no app handler", hook: true, wantSIGTERM: true, wantSIGINT: true},
		{
			name: "app handles SIGTERM", hook: true,
			appSignals:  [][]os.Signal{{syscall.SIGTERM}},
			wantSIGTERM: false, wantSIGINT: true,
		},
		{
			name: "app handles every signal", hook: true,
			appSignals:  [][]os.Signal{{}},
			wantSIGTERM: false, wantSIGINT: false,
		},
		{
			name: "app handles another signal", hook: true,
			appSignals:  [][]os.Signal{{syscall.SIGHUP}},
			wantSIGTERM: true, wantSIGINT: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetSignalOwners(t)
			otelcCh := make(chan os.Signal, 1)
			signalOwners.otelcCh = otelcCh
			if tt.hook {
				RecordSignalNotify(otelcCh, shutdownSignals())
			}
			for _, sigs := range tt.appSignals {
				RecordSignalNotify(make(chan os.Signal, 1), sigs)
			}

			signalOwners.Lock()
			defer signalOwners.Unlock()
			assert.Equal(t, tt.wantSIGTERM, shouldReraise(syscall.SIGTERM))
			assert.Equal(t, tt.wantSIGINT, shouldReraise(os.Interrupt))
		})
	}
}

// sliceSignal is an os.Signal that cannot be used as a map key.
type sliceSignal []int

func (sliceSignal) String() string { return "slice" }
func (sliceSignal) Signal()        {}

func TestRecordSignalNotifyUnknownSignalType(t *testing.T) {
	resetSignalOwners(t)
	assert.NotPanics(t, func() {
		RecordSignalNotify(make(chan os.Signal, 1), []os.Signal{sliceSignal{1}})
	})
}

// TestHandleShutdownSignalReraise delivers a real SIGTERM to a child process
// and checks how the child ends.
func TestHandleShutdownSignalReraise(t *testing.T) {
	const appReceived = "otelc-app-received-signal"
	if mode := os.Getenv("OTELC_RERAISE_PROBE"); mode != "" {
		runReraiseProbe(mode, appReceived)
		return
	}
	if stdruntime.GOOS == "windows" {
		t.Skip("Unix signals are not supported on windows")
	}

	run := func(t *testing.T, mode string) (string, error) {
		t.Helper()
		cmd := exec.Command(os.Args[0], "-test.run=^TestHandleShutdownSignalReraise$")
		cmd.Env = append(os.Environ(), "OTELC_RERAISE_PROBE="+mode)
		var stdout bytes.Buffer
		cmd.Stdout = &stdout
		err := cmd.Run()
		return stdout.String(), err
	}

	t.Run("no app handler", func(t *testing.T) {
		_, err := run(t, "none")
		var exitErr *exec.ExitError
		require.ErrorAs(t, err, &exitErr, "the re-raised SIGTERM should terminate the process")
		status, ok := exitErr.Sys().(syscall.WaitStatus)
		require.True(t, ok)
		require.True(t, status.Signaled(), "the process should end by the signal, not exit normally")
		assert.Equal(t, syscall.SIGTERM, status.Signal())
	})

	t.Run("app handler", func(t *testing.T) {
		stdout, err := run(t, "notify")
		require.NoError(t, err, "otelc must not terminate a process that handles the signal")
		assert.Equal(t, 1, strings.Count(stdout, appReceived),
			"the application should receive the signal exactly once")
	})

	// signal.NotifyContext callers often call stop() as soon as the signal
	// arrives and then drain. A re-raise at that point would kill them.
	t.Run("app handler stopped", func(t *testing.T) {
		_, err := run(t, "stopped")
		require.NoError(t, err, "otelc must not re-raise for an application that registered a handler")
	})

	t.Run("signal ignored before setup", func(t *testing.T) {
		_, err := run(t, "ignored")
		require.NoError(t, err, "otelc must not re-raise a signal the application ignores")
	})
}

// runReraiseProbe runs in the child process of TestHandleShutdownSignalReraise.
// It calls RecordSignalNotify the way the os/signal instrumentation does.
func runReraiseProbe(mode, appReceived string) {
	if mode == "ignored" {
		signal.Ignore(syscall.SIGTERM)
		setupSignalHandler()
		// Unit tests are not instrumented, so mark the hook as applied by hand.
		signalOwners.Lock()
		signalOwners.hookInstalled = true
		signalOwners.Unlock()
		sendSIGTERM()
		// Give the handler time to flush and to re-raise if it wrongly does.
		time.Sleep(time.Second)
		return
	}

	otelcCh := make(chan os.Signal, 1)
	signalOwners.Lock()
	signalOwners.otelcCh = otelcCh
	signalOwners.Unlock()
	RecordSignalNotify(otelcCh, shutdownSignals())
	signal.Notify(otelcCh, shutdownSignals()...)

	appCh := make(chan os.Signal, 2)
	if mode != "none" {
		RecordSignalNotify(appCh, []os.Signal{syscall.SIGTERM})
		signal.Notify(appCh, syscall.SIGTERM)
	}
	if mode == "stopped" {
		signal.Stop(appCh)
	}

	sendSIGTERM()
	handleShutdownSignal(otelcCh)

	if mode == "notify" {
		<-appCh
		fmt.Println(appReceived)
		// A re-raised signal would arrive as a second one.
		select {
		case <-appCh:
			fmt.Println(appReceived)
		case <-time.After(200 * time.Millisecond):
		}
	}
	// Give a re-raised signal time to arrive before the child exits normally.
	time.Sleep(200 * time.Millisecond)
}

func TestLoggerOutput(t *testing.T) {
	const probe = "otelc-logger-probe"
	if os.Getenv("OTELC_LOGGER_PROBE") == "1" {
		Logger().Error(probe)
		return
	}

	tests := []struct {
		level      string
		wantStderr bool
	}{
		{"", false},
		{"none", false},
		{"error", true},
	}
	for _, tt := range tests {
		t.Run("level="+tt.level, func(t *testing.T) {
			// Re-run this test in a child process so the logger built by init()
			// is the one under test, with stdout and stderr captured separately.
			cmd := exec.Command(os.Args[0], "-test.run=^TestLoggerOutput$")
			cmd.Env = append(os.Environ(), "OTELC_LOGGER_PROBE=1", "OTEL_LOG_LEVEL="+tt.level)
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			require.NoError(t, cmd.Run())

			assert.NotContains(t, stdout.String(), probe, "SDK logs must not be written to stdout")
			if tt.wantStderr {
				assert.Contains(t, stderr.String(), probe, "SDK logs should be written to stderr")
			} else {
				assert.NotContains(t, stderr.String(), probe, "SDK logs should be off by default")
			}
		})
	}
}

func TestLogLevel(t *testing.T) {
	tests := []struct {
		envValue string
		expected slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"Debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"Info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{" WARN ", slog.LevelWarn},
		{"error", slog.LevelError},
		{"ERROR", slog.LevelError},
		{"none", levelNone},
		{"NONE", levelNone},
		{"", levelNone},        // unset turns logging off
		{"unknown", levelNone}, // unrecognized value turns logging off
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

type errLogProcessor struct{ err error }

func (errLogProcessor) Enabled(context.Context, sdklog.EnabledParameters) bool { return true }
func (errLogProcessor) OnEmit(context.Context, *sdklog.Record) error           { return nil }
func (p errLogProcessor) Shutdown(context.Context) error                       { return p.err }
func (errLogProcessor) ForceFlush(context.Context) error                       { return nil }

type customErrSpanProcessor struct{ err error }

func (customErrSpanProcessor) OnStart(context.Context, sdktrace.ReadWriteSpan) {}
func (customErrSpanProcessor) OnEnd(sdktrace.ReadOnlySpan)                     {}
func (customErrSpanProcessor) ForceFlush(context.Context) error                { return nil }
func (p customErrSpanProcessor) Shutdown(context.Context) error                { return p.err }

func TestShutdownMultipleErrors(t *testing.T) {
	origTracer, origMeter, origLogger := tracerProvider, meterProvider, loggerProvider
	t.Cleanup(func() { tracerProvider, meterProvider, loggerProvider = origTracer, origMeter, origLogger })

	tracerErr := errors.New("tracer shutdown failed")
	loggerErr := errors.New("logger shutdown failed")

	tracerProvider = sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(customErrSpanProcessor{err: tracerErr}))
	meterProvider = nil
	loggerProvider = sdklog.NewLoggerProvider(sdklog.WithProcessor(errLogProcessor{err: loggerErr}))

	err := Shutdown(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, tracerErr)
	assert.ErrorIs(t, err, loggerErr)
}

// restoreProviders resets the global providers after a test that configures them.
func restoreProviders(t *testing.T) {
	origTracer, origMeter, origLogger := tracerProvider, meterProvider, loggerProvider
	t.Cleanup(func() { tracerProvider, meterProvider, loggerProvider = origTracer, origMeter, origLogger })
}

func TestNewResourceIncludesTelemetrySDK(t *testing.T) {
	// WithFromEnv runs last in newResource, so OTEL_RESOURCE_ATTRIBUTES can
	// override these values. Pin it to keep the test hermetic.
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "")

	res := newResource(context.Background())
	set := res.Set()

	name, ok := set.Value(attribute.Key("telemetry.sdk.name"))
	require.True(t, ok, "auto-configured resource must set telemetry.sdk.name")
	assert.Equal(t, "opentelemetry", name.AsString())

	lang, ok := set.Value(attribute.Key("telemetry.sdk.language"))
	require.True(t, ok, "auto-configured resource must set telemetry.sdk.language")
	assert.Equal(t, "go", lang.AsString())

	ver, ok := set.Value(attribute.Key("telemetry.sdk.version"))
	require.True(t, ok, "auto-configured resource must set telemetry.sdk.version")
	assert.NotEmpty(t, ver.AsString())

	// resource.Default() (the fallback in newResource) also stamps the
	// telemetry.sdk.* attributes, so the assertions above cannot tell the
	// happy path from the fallback. process.pid is only set on the happy
	// path and the environment cannot remove it.
	pid, ok := set.Value(attribute.Key("process.pid"))
	require.True(t, ok, "resource must include detector attributes; this fails if newResource fell back to resource.Default()")
	assert.NotZero(t, pid.AsInt64())
}

func TestNewResourceIncludesService(t *testing.T) {
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "")
	t.Setenv("OTEL_SERVICE_NAME", "")

	res := newResource(context.Background())
	set := res.Set()

	name, ok := set.Value(attribute.Key("service.name"))
	require.True(t, ok, "auto-configured resource must set service.name")
	assert.True(t, strings.HasPrefix(name.AsString(), "unknown_service:"),
		"default service.name = %q, want unknown_service:<executable>", name.AsString())

	id, ok := set.Value(attribute.Key("service.instance.id"))
	require.True(t, ok, "auto-configured resource must set service.instance.id")
	assert.NotEmpty(t, id.AsString())
}

func TestNewResourceServiceNameFromEnv(t *testing.T) {
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "")
	t.Setenv("OTEL_SERVICE_NAME", "checkout")

	res := newResource(context.Background())
	name, ok := res.Set().Value(attribute.Key("service.name"))
	require.True(t, ok)
	assert.Equal(t, "checkout", name.AsString())
}

func TestNewResourceFallsBackToDefaultOnError(t *testing.T) {
	// A pair missing "=" makes resource.New fail with ErrPartialResource,
	// so newResource must warn and return resource.Default().
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "key")

	var buf bytes.Buffer
	origLogger := logger
	t.Cleanup(func() { logger = origLogger })
	logger = slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	res := newResource(context.Background())
	_, ok := res.Set().Value(attribute.Key("service.name"))
	require.True(t, ok, "fallback resource should be resource.Default(), which sets service.name")
	assert.Contains(t, buf.String(), "failed to create resource")
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
	assert.Contains(t, fields, "b3",
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

func TestUseSimpleSpanProcessor(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		want     bool
	}{
		{"literal true", "true", true},
		{"uppercase TRUE", "TRUE", true},
		{"mixed case True", "True", true},
		{"literal false", "false", false},
		{"arbitrary value", "1", false},
		{"unset", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("OTEL_GO_SIMPLE_SPAN_PROCESSOR", tt.envValue)
			assert.Equal(t, tt.want, useSimpleSpanProcessor())
		})
	}
}

func TestNewSpanProcessorUsesSimpleSpanProcessor(t *testing.T) {
	t.Setenv("OTEL_GO_SIMPLE_SPAN_PROCESSOR", "TRUE")
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(newSpanProcessor(exporter)))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })

	_, span := provider.Tracer("test").Start(context.Background(), "test")
	span.End()

	assert.Len(t, exporter.GetSpans(), 1, "simple processor should export completed spans immediately")
}

// batchRecorder records the size of every batch passed to ExportSpans.
type batchRecorder struct {
	mu      sync.Mutex
	batches []int
}

func (r *batchRecorder) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.batches = append(r.batches, len(spans))
	return nil
}

func (*batchRecorder) Shutdown(context.Context) error { return nil }

func (r *batchRecorder) exported() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]int(nil), r.batches...)
}

func TestNewSpanProcessorRespectsMaxExportBatchSizeEnv(t *testing.T) {
	t.Setenv("OTEL_BSP_MAX_EXPORT_BATCH_SIZE", "2")
	// Long schedule delay so only the batch size triggers exports before ForceFlush.
	t.Setenv("OTEL_BSP_SCHEDULE_DELAY", "3600000")
	exporter := &batchRecorder{}
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(newSpanProcessor(exporter)))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })

	for range 5 {
		_, span := provider.Tracer("test").Start(context.Background(), "test")
		span.End()
	}
	require.NoError(t, provider.ForceFlush(context.Background()))

	batches := exporter.exported()
	total := 0
	for _, n := range batches {
		assert.LessOrEqual(t, n, 2, "batch exceeds OTEL_BSP_MAX_EXPORT_BATCH_SIZE")
		total += n
	}
	assert.Equal(t, 5, total)
}

func TestNewSpanProcessorRespectsScheduleDelayEnv(t *testing.T) {
	t.Setenv("OTEL_BSP_SCHEDULE_DELAY", "10")
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(newSpanProcessor(exporter)))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })

	_, span := provider.Tracer("test").Start(context.Background(), "test")
	span.End()

	// Well under both the previous hardcoded 1s and the SDK default 5s, so this
	// only passes if OTEL_BSP_SCHEDULE_DELAY is honored.
	assert.Eventually(t, func() bool { return len(exporter.GetSpans()) == 1 },
		500*time.Millisecond, 5*time.Millisecond)
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

func sendSIGTERM() {
	p, err := os.FindProcess(os.Getpid())
	if err != nil {
		panic(err)
	}
	if err := p.Signal(syscall.SIGTERM); err != nil {
		panic(err)
	}
}
