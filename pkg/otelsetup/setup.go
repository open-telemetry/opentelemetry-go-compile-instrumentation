// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package otelsetup

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/contrib/exporters/autoexport"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

const (
	// Default export intervals and batch sizes
	defaultTraceBatchTimeout = 5 * time.Second
	defaultMetricInterval    = 10 * time.Second
	defaultTraceBatchSize    = 512
)

var (
	logger         *slog.Logger
	meterProvider  *sdkmetric.MeterProvider
	tracerProvider *sdktrace.TracerProvider
	initOnce       sync.Once
)

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

		// Initialize logger
		logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: getLogLevel(),
		}))

		// Setup OpenTelemetry
		if err := setupOpenTelemetry(cfg); err != nil {
			logger.Error("failed to setup OpenTelemetry", "error", err)
		}
	})
}

// GetLogger returns the package logger
func GetLogger() *slog.Logger {
	if logger == nil {
		logger = slog.Default()
	}
	return logger
}

// GetMeterProvider returns the meter provider
func GetMeterProvider() *sdkmetric.MeterProvider {
	if meterProvider == nil {
		// Return a no-op meter provider if not initialized
		meterProvider = sdkmetric.NewMeterProvider()
	}
	return meterProvider
}

// GetTracerProvider returns the tracer provider
func GetTracerProvider() *sdktrace.TracerProvider {
	if tracerProvider == nil {
		// Return a no-op tracer provider if not initialized
		tracerProvider = sdktrace.NewTracerProvider()
	}
	return tracerProvider
}

// getLogLevel returns the log level from environment variable
func getLogLevel() slog.Level {
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

	// Get service name from environment or use config default
	serviceName := os.Getenv("OTEL_SERVICE_NAME")
	if serviceName == "" {
		serviceName = cfg.ServiceName
	}
	if serviceName == "" {
		serviceName = "otel-instrumentation"
	}

	// Determine service version
	serviceVersion := cfg.ServiceVersion
	if serviceVersion == "" {
		serviceVersion = cfg.InstrumentationVersion
	}

	// Create resource with service information
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			semconv.ServiceVersionKey.String(serviceVersion),
		),
		resource.WithFromEnv(),
		resource.WithProcess(),
		resource.WithOS(),
		resource.WithContainer(),
		resource.WithHost(),
	)
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

	// Start runtime metrics instrumentation
	if err := runtime.Start(runtime.WithMeterProvider(GetMeterProvider())); err != nil {
		logger.Warn("failed to start runtime metrics", "error", err)
	} else {
		logger.Info("runtime metrics enabled")
	}

	logger.Info("OpenTelemetry initialized",
		"service_name", serviceName,
		"instrumentation_name", cfg.InstrumentationName,
		"instrumentation_version", cfg.InstrumentationVersion)

	return nil
}

// stripScheme removes http:// or https:// prefix from an endpoint for gRPC
func stripScheme(endpoint string) string {
	endpoint = strings.TrimPrefix(endpoint, "http://")
	endpoint = strings.TrimPrefix(endpoint, "https://")
	return endpoint
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

	// Strip http:// or https:// prefix for gRPC (gRPC expects host:port only)
	grpcEndpoint := stripScheme(endpoint)

	// Create OTLP trace exporter
	traceExporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(grpcEndpoint),
		otlptracegrpc.WithInsecure(), // Use insecure for demo purposes
	)
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
			GetLogger().Error("failed to shutdown tracer provider", "error", shutdownErr)
			err = shutdownErr
		}
	}

	if meterProvider != nil {
		if shutdownErr := meterProvider.Shutdown(ctx); shutdownErr != nil {
			GetLogger().Error("failed to shutdown meter provider", "error", shutdownErr)
			err = shutdownErr
		}
	}

	return err
}
