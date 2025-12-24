// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package shared

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"time"

	"go.opentelemetry.io/contrib/exporters/autoexport"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

const (
	// Default export intervals and batch sizes
	defaultTraceBatchTimeout = 5 * time.Second
	defaultTraceBatchSize    = 512
)

var (
	logger             *slog.Logger
	meterProvider      *sdkmetric.MeterProvider
	tracerProvider     *sdktrace.TracerProvider
	initOnce           sync.Once
	runtimeMetricsOnce sync.Once
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
	ServiceName            string
	ServiceVersion         string
	InstrumentationName    string
	InstrumentationVersion string
}

// Initialize sets up OpenTelemetry with defensive error handling
// This function is safe to call multiple times - it will only initialize once
func Initialize(cfg Config) {
	initOnce.Do(func() {
		// Defensive: ensure instrumentation initialization never crashes user application
		defer func() {
			if rec := recover(); rec != nil {
				// Log panic but don't propagate - user application must continue
				if logger != nil {
					logger.Error("panic during OpenTelemetry initialization", "panic", rec)
				} else {
					// Fallback if logger isn't initialized
					slog.Default().Error("panic during OpenTelemetry initialization", "panic", rec)
				}
			}
		}()

		// Setup OpenTelemetry
		if err := setupOpenTelemetry(cfg); err != nil {
			logger.Error("failed to setup OpenTelemetry", "error", err)
		}

		// Setup automatic shutdown on signals
		setupSignalHandler()
	})
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
func setupOpenTelemetry(cfg Config) (retErr error) {
	// Defensive: catch any panics during setup
	defer func() {
		if rec := recover(); rec != nil {
			logger.Error("panic during OpenTelemetry setup", "panic", rec)
			retErr = nil // Don't propagate error, just log it
		}
	}()

	ctx := context.Background()

	// Build resource with proper precedence:
	// 1. OTEL_RESOURCE_ATTRIBUTES (highest precedence - handled by WithFromEnv)
	// 2. OTEL_SERVICE_NAME (handled by WithFromEnv)
	// 3. Fallback defaults (lowest precedence)
	//
	// Per OTel spec, environment variables should override code configuration.
	// We achieve this by putting WithFromEnv() AFTER explicit attributes,
	// which causes env vars to take precedence.
	var resourceOptions []resource.Option

	// Start with detectors that don't conflict with service.* attributes
	resourceOptions = append(resourceOptions,
		resource.WithProcess(),
		resource.WithOS(),
		resource.WithContainer(),
		resource.WithHost(),
	)

	// Add fallback defaults for service.name and service.version
	// These will be overridden by environment variables if present
	serviceName := os.Getenv("OTEL_SERVICE_NAME")
	if serviceName == "" {
		serviceName = cfg.ServiceName
	}

	// Only set service.version if we have a meaningful value
	// Environment variables (via WithFromEnv) will override this if present
	if cfg.ServiceVersion != "" {
		resourceOptions = append(resourceOptions,
			resource.WithAttributes(
				semconv.ServiceNameKey.String(serviceName),
				semconv.ServiceVersionKey.String(cfg.ServiceVersion),
			),
		)
	}

	// Add environment-based configuration LAST so it takes precedence
	// This will respect OTEL_RESOURCE_ATTRIBUTES and OTEL_SERVICE_NAME
	resourceOptions = append(resourceOptions, resource.WithFromEnv())

	// Create resource
	res, err := resource.New(ctx, resourceOptions...)
	if err != nil {
		// Log but don't fail - continue with basic providers
		logger.Warn("failed to create resource", "error", err)
		res = resource.Default()
	}

	// Setup trace provider with OTLP exporter
	if err := setupTraceProvider(ctx, res); err != nil {
		logger.Warn("failed to setup trace provider", "error", err)
	}

	// Setup meter provider with OTLP exporter
	if err := setupMeterProvider(ctx, res); err != nil {
		logger.Warn("failed to setup meter provider", "error", err)
	}

	// Set W3C Trace Context as the propagator
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	logger.Info("OpenTelemetry initialized",
		"service_name", serviceName,
		"instrumentation_name", cfg.InstrumentationName,
		"instrumentation_version", cfg.InstrumentationVersion)

	return nil
}

// setupTraceProvider creates and configures the trace provider
func setupTraceProvider(ctx context.Context, res *resource.Resource) error {
	// Get OTLP endpoint from environment
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		endpoint = os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")
	}

	// If no endpoint is configured, skip trace provider setup
	if endpoint == "" {
		logger.Debug("no OTLP endpoint configured, skipping trace provider setup")
		return nil
	}

	// Use autoexport to automatically select the right exporter based on
	// OTEL_EXPORTER_OTLP_PROTOCOL (defaults to http/protobuf)
	traceExporter, err := autoexport.NewSpanExporter(ctx)
	if err != nil {
		return err
	}

	// Create trace provider with batch span processor
	tracerProvider = sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(traceExporter,
			sdktrace.WithBatchTimeout(defaultTraceBatchTimeout),
			sdktrace.WithMaxExportBatchSize(defaultTraceBatchSize),
		),
	)

	// Set global tracer provider
	otel.SetTracerProvider(tracerProvider)

	logger.Info("trace provider initialized", "endpoint", endpoint)
	return nil
}

// setupMeterProvider creates and configures the meter provider
func setupMeterProvider(ctx context.Context, res *resource.Resource) error {
	// Use autoexport to automatically select the right exporter based on
	// OTEL_EXPORTER_OTLP_PROTOCOL (defaults to http/protobuf)
	// Supports: otlp, console, and none
	metricReader, err := autoexport.NewMetricReader(ctx)
	if err != nil {
		return err
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
// This function is safe to call multiple times - it will only start runtime metrics once.
// Each instrumentation package calls this during initialization to ensure runtime metrics
// are available when the application uses any instrumentation.
//
// Returns error if runtime metrics fail to start, but this is non-fatal.
func StartRuntimeMetrics() error {
	var startErr error

	runtimeMetricsOnce.Do(func() {
		// Check if runtime metrics are enabled
		if !Instrumented("runtimemetrics") {
			logger.Debug("runtime metrics disabled via environment variable")
			return
		}

		// Get the meter provider from the global registry
		mp := otel.GetMeterProvider()

		if err := runtime.Start(runtime.WithMeterProvider(mp)); err != nil {
			logger.Warn("failed to start runtime metrics", "error", err)
			startErr = err
			return
		}

		logger.Info("runtime metrics enabled")
	})

	return startErr
}

// setupSignalHandler registers a goroutine that listens for OS signals
// and gracefully shuts down the OpenTelemetry SDK when receiving interrupt signals.
// This ensures telemetry is flushed before the application exits.
func setupSignalHandler() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)

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

		// After shutdown completes, exit cleanly
		// os.Interrupt is cross-platform (SIGINT on Unix, Ctrl+C on Windows)
		signal.Reset(os.Interrupt)
		os.Exit(0)
	}()
}
