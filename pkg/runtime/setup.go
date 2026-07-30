// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/exporters/autoexport"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/contrib/propagators/autoprop"
	"go.opentelemetry.io/otel"
	logglobal "go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

const (
	// Default export intervals and batch sizes
	defaultTraceBatchTimeout = time.Second
	defaultTraceBatchSize    = 512
)

var (
	logger                *slog.Logger
	meterProvider         *sdkmetric.MeterProvider
	tracerProvider        *sdktrace.TracerProvider
	loggerProvider        *sdklog.LoggerProvider
	registerSignalHandler sync.Once
)

func init() {
	// Initialize logger early so hook packages can use it with the correct log level
	// This is called at package load time, before any hooks execute
	logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel(),
	}))
}

// Config holds configuration for OpenTelemetry setup
type Config struct {
	InstrumentationName    string
	InstrumentationVersion string
}

// Initialize sets up OpenTelemetry with defensive error handling
func Initialize(cfg Config) {
	// Defensive: ensure instrumentation initialization never crashes user application
	defer func() {
		if rec := recover(); rec != nil {
			// Log panic but don't propagate - user application must continue
			Logger().Error("panic during OpenTelemetry initialization", "panic", rec)
		}
	}()

	// Setup OpenTelemetry
	setupOpenTelemetry(cfg)

	// Setup automatic shutdown on signals
	setupSignalHandler()
}

// Logger returns the package logger
func Logger() *slog.Logger {
	if logger == nil {
		logger = slog.Default()
	}
	return logger
}

// logLevel returns the log level from environment variable
func logLevel() slog.Level {
	levelStr := os.Getenv("OTEL_LOG_LEVEL")
	switch levelStr {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// setupOpenTelemetry initializes the OpenTelemetry SDK with OTLP exporters
func setupOpenTelemetry(cfg Config) {
	// Defensive: catch any panics during setup
	defer func() {
		if rec := recover(); rec != nil {
			logger.Error("panic during OpenTelemetry setup", "panic", rec)
		}
	}()

	// The default handler writes to the stdlib logger on stderr, which bypasses OTEL_LOG_LEVEL and does not match
	// the structured output the rest of the runtime emits.
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		logger.Error("OpenTelemetry SDK error", "error", err)
	}))

	ctx := context.Background()

	// Create resource
	res, err := resource.New(ctx, resource.WithProcess(),
		resource.WithOS(),
		resource.WithContainer(),
		resource.WithHost(),
		resource.WithFromEnv())
	if err != nil {
		// Log but don't fail - continue with basic providers
		logger.Warn("failed to create resource", "error", err)
		res = resource.Default()
	}

	// Setup trace provider with auto-configured exporter
	if err := setupTraceProvider(ctx, res); err != nil {
		logger.Warn("failed to setup trace provider", "error", err)
	}

	// Setup meter provider with auto-configured exporter
	if err := setupMeterProvider(ctx, res); err != nil {
		logger.Warn("failed to setup meter provider", "error", err)
	}

	// Setup logger provider with auto-configured exporter
	if err := setupLoggerProvider(ctx, res); err != nil {
		logger.Warn("failed to setup logger provider", "error", err)
	}

	// Use autoprop to select propagators from OTEL_PROPAGATORS (tracecontext,
	// baggage, b3, b3multi, jaeger, xray, ottrace, none). Defaults to W3C
	// Trace Context + Baggage when the variable is not set.
	otel.SetTextMapPropagator(autoprop.NewTextMapPropagator())

	logger.Info("OpenTelemetry initialized",
		"instrumentation_name", cfg.InstrumentationName,
		"instrumentation_version", cfg.InstrumentationVersion)
}

// setupTraceProvider creates and configures the trace provider
func setupTraceProvider(ctx context.Context, res *resource.Resource) error {
	// Use autoexport to automatically select the right exporter based on
	// OTEL_TRACES_EXPORTER (defaults to otlp) and OTEL_EXPORTER_OTLP_PROTOCOL
	// (defaults to http/protobuf). Supports: otlp, console, and none.
	//
	// This mirrors setupMeterProvider: the exporter-selection env vars decide
	// whether/where traces go, not the presence of an OTLP endpoint env var.
	// When otlp is selected (the default) and no endpoint is configured, the
	// exporter falls back to the OTLP-spec default endpoint.
	traceExporter, err := autoexport.NewSpanExporter(ctx)
	if err != nil {
		return err
	}

	// OTEL_TRACES_EXPORTER=none: skip building a provider/processor entirely
	// rather than running a batch processor that will never export anything.
	if autoexport.IsNoneSpanExporter(traceExporter) {
		logger.Debug("trace exporter disabled via OTEL_TRACES_EXPORTER=none, skipping trace provider setup")
		return nil
	}

	spanProcessor := sdktrace.NewBatchSpanProcessor(traceExporter,
		sdktrace.WithBatchTimeout(defaultTraceBatchTimeout),
		sdktrace.WithMaxExportBatchSize(defaultTraceBatchSize),
	)
	if os.Getenv("OTEL_GO_SIMPLE_SPAN_PROCESSOR") == "true" {
		spanProcessor = sdktrace.NewSimpleSpanProcessor(traceExporter)
		logger.Debug("using SimpleSpanProcessor for immediate span export")
	}

	tracerProvider = sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(spanProcessor),
	)

	// Set global tracer provider
	otel.SetTracerProvider(tracerProvider)

	logger.Info("trace provider initialized with auto-export")
	return nil
}

// setupMeterProvider creates and configures the meter provider
func setupMeterProvider(ctx context.Context, res *resource.Resource) error {
	// Use autoexport to automatically select the right exporter based on
	// OTEL_METRICS_EXPORTER (defaults to otlp) and OTEL_EXPORTER_OTLP_PROTOCOL
	// (defaults to http/protobuf). Supports: otlp, console, prometheus, and none.
	metricReader, err := autoexport.NewMetricReader(ctx)
	if err != nil {
		return err
	}

	// OTEL_METRICS_EXPORTER=none: skip building a provider around a reader
	// that will never be collected, matching the traces/logs behavior.
	if autoexport.IsNoneMetricReader(metricReader) {
		logger.Debug("metric exporter disabled via OTEL_METRICS_EXPORTER=none, skipping meter provider setup")
		return nil
	}

	// Create meter provider with the auto-configured reader
	meterProvider = sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(metricReader),
	)

	// Set global meter provider
	otel.SetMeterProvider(meterProvider)

	logger.Info("meter provider initialized with auto-export")
	return nil
}

// setupLoggerProvider creates and configures the logger provider
func setupLoggerProvider(ctx context.Context, res *resource.Resource) error {
	// Use autoexport to automatically select the right exporter based on
	// OTEL_LOGS_EXPORTER (defaults to otlp) and OTEL_EXPORTER_OTLP_PROTOCOL
	// (defaults to http/protobuf). Supports: otlp, console, and none.
	//
	// This mirrors setupTraceProvider/setupMeterProvider so all three signals
	// are configured symmetrically via autoexport.
	logExporter, err := autoexport.NewLogExporter(ctx)
	if err != nil {
		return err
	}

	// OTEL_LOGS_EXPORTER=none: skip building a provider/processor entirely
	// rather than running a batch processor that will never export anything.
	if autoexport.IsNoneLogExporter(logExporter) {
		logger.Debug("log exporter disabled via OTEL_LOGS_EXPORTER=none, skipping logger provider setup")
		return nil
	}

	loggerProvider = sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
	)

	// Set global logger provider
	logglobal.SetLoggerProvider(loggerProvider)

	logger.Info("logger provider initialized with auto-export")
	return nil
}

// Shutdown gracefully shuts down the OpenTelemetry SDK
func Shutdown(ctx context.Context) error {
	var err error

	if tracerProvider != nil {
		if shutdownErr := tracerProvider.Shutdown(ctx); shutdownErr != nil {
			Logger().Error("failed to shutdown tracer provider", "error", shutdownErr)
			err = shutdownErr
		}
	}

	if meterProvider != nil {
		if shutdownErr := meterProvider.Shutdown(ctx); shutdownErr != nil {
			Logger().Error("failed to shutdown meter provider", "error", shutdownErr)
			err = shutdownErr
		}
	}

	if loggerProvider != nil {
		if shutdownErr := loggerProvider.Shutdown(ctx); shutdownErr != nil {
			Logger().Error("failed to shutdown logger provider", "error", shutdownErr)
			err = shutdownErr
		}
	}

	return err
}

// StartRuntimeMetrics enables Go runtime metrics collection.
// This follows the same enable/disable pattern as other instrumentations via
// OTEL_GO_ENABLED_INSTRUMENTATIONS and OTEL_GO_DISABLED_INSTRUMENTATIONS.
//
// Runtime metrics are enabled by default. To disable:
//   - Set OTEL_GO_DISABLED_INSTRUMENTATIONS=runtimemetrics
//   - Or set OTEL_GO_ENABLED_INSTRUMENTATIONS without including "runtimemetrics"
//
// Returns error if runtime metrics fail to start, but this is non-fatal.
func StartRuntimeMetrics() {
	if !Instrumented("runtimemetrics") {
		logger.Debug("runtime metrics disabled via environment variable")
		return
	}

	// Get the meter provider from the global registry
	mp := otel.GetMeterProvider()

	if err := runtime.Start(runtime.WithMeterProvider(mp)); err != nil {
		logger.Warn("failed to start runtime metrics", "error", err)
		return
	}

	logger.Info("runtime metrics enabled")
}

// shutdownSignals returns the OS signals that trigger graceful OTel shutdown.
func shutdownSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}

// setupSignalHandler registers a goroutine that listens for OS signals
// and gracefully shuts down the OpenTelemetry SDK when receiving SIGINT or SIGTERM.
// This ensures telemetry is flushed before the application exits.
// This function is safe to call multiple times; it will only register the handler once.
func setupSignalHandler() {
	registerSignalHandler.Do(func() {
		sigCh := make(chan os.Signal, 1)
		signals := shutdownSignals()
		signal.Notify(sigCh, signals...)

		go func() {
			sig := <-sigCh
			logger.Info("received signal, initiating graceful shutdown", "signal", sig.String())

			// Create a context with timeout for shutdown
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Shutdown OTel SDK
			if err := Shutdown(ctx); err != nil {
				logger.Error("error during shutdown", "error", err)
			} else {
				logger.Info("OpenTelemetry SDK shutdown completed successfully")
			}

			// After shutdown completes, exit cleanly.
			signal.Reset(signals...)
			os.Exit(0)
		}()
	})
}
