// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"os"
	"os/signal"
	"strings"
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
	// Stderr, not stdout: stdout belongs to the instrumented program and may carry data.
	logger = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
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

// levelNone is above every slog level, so a handler set to it logs nothing.
const levelNone = slog.Level(math.MaxInt)

// logLevel returns the log level from OTEL_LOG_LEVEL. Logging is off unless the
// variable is set to a known level.
func logLevel() slog.Level {
	levelStr := strings.ToLower(strings.TrimSpace(os.Getenv("OTEL_LOG_LEVEL")))
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
		return levelNone
	}
}

// newResource builds the process resource for auto-configured providers.
// WithTelemetrySDK stamps telemetry.sdk.name/language/version, which
// resource.Default() includes but resource.New() does not unless asked.
// WithService stamps service.name (unknown_service:<executable>) and
// service.instance.id. WithFromEnv stays last so OTEL_SERVICE_NAME and
// OTEL_RESOURCE_ATTRIBUTES still win.
func newResource(ctx context.Context) *resource.Resource {
	res, err := resource.New(ctx,
		resource.WithTelemetrySDK(),
		resource.WithService(),
		resource.WithProcess(),
		resource.WithOS(),
		resource.WithContainer(),
		resource.WithHost(),
		resource.WithFromEnv())
	if err != nil {
		logger.Warn("failed to create resource", "error", err)
		return resource.Default()
	}
	return res
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
	res := newResource(ctx)

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

	spanProcessor := newSpanProcessor(traceExporter)

	tracerProvider = sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(spanProcessor),
	)

	// Set global tracer provider
	otel.SetTracerProvider(tracerProvider)

	logger.Info("trace provider initialized with auto-export")
	return nil
}

func newSpanProcessor(traceExporter sdktrace.SpanExporter) sdktrace.SpanProcessor {
	if useSimpleSpanProcessor() {
		logger.Debug("using SimpleSpanProcessor for immediate span export")
		return sdktrace.NewSimpleSpanProcessor(traceExporter)
	}

	// No explicit options: NewBatchSpanProcessor already reads the standard
	// OTEL_BSP_* env vars and falls back to the SDK defaults.
	return sdktrace.NewBatchSpanProcessor(traceExporter)
}

func useSimpleSpanProcessor() bool {
	return strings.EqualFold(os.Getenv("OTEL_GO_SIMPLE_SPAN_PROCESSOR"), "true")
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

type lifecycleProvider interface {
	ForceFlush(context.Context) error
	Shutdown(context.Context) error
}

type namedProvider struct {
	name     string
	provider lifecycleProvider
}

func configuredProviders() []namedProvider {
	const signalCount = 3
	providers := make([]namedProvider, 0, signalCount)
	if tracerProvider != nil {
		providers = append(providers, namedProvider{name: "tracer", provider: tracerProvider})
	}
	if meterProvider != nil {
		providers = append(providers, namedProvider{name: "meter", provider: meterProvider})
	}
	if loggerProvider != nil {
		providers = append(providers, namedProvider{name: "logger", provider: loggerProvider})
	}
	return providers
}

func runProviderOp(
	ctx context.Context,
	action string,
	op func(lifecycleProvider, context.Context) error,
) error {
	var errs []error
	for _, p := range configuredProviders() {
		if err := op(p.provider, ctx); err != nil {
			Logger().ErrorContext(ctx, "failed to "+action+" "+p.name+" provider", "error", err)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// ForceFlush immediately flushes any buffered telemetry from the OpenTelemetry SDK providers.
// It skips nil providers, logs individual failures through Logger(), joins any errors,
// and leaves providers active so callers can keep recording and flushing telemetry.
func ForceFlush(ctx context.Context) error {
	return runProviderOp(ctx, "force flush", lifecycleProvider.ForceFlush)
}

// Shutdown gracefully shuts down the OpenTelemetry SDK
func Shutdown(ctx context.Context) error {
	return runProviderOp(ctx, "shutdown", lifecycleProvider.Shutdown)
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

// setupSignalHandler flushes the OTel SDK on SIGINT/SIGTERM so buffered telemetry
// survives shutdown. It never exits. It re-raises the signal only when the
// application never registered a handler for it (see shouldReraise), so the
// process terminates the way it would without otelc. An application that
// handles the signal owns its exit path and exit code: re-raising there would
// race its own graceful shutdown and could truncate it.
func setupSignalHandler() {
	registerSignalHandler.Do(func() {
		sigCh := make(chan os.Signal, 1)
		signalOwners.Lock()
		signalOwners.otelcCh = sigCh
		// otelc's signal.Notify below turns an ignored signal back on. Treat
		// the application as owning such a signal so otelc never re-raises it.
		for _, s := range shutdownSignals() {
			if signal.Ignored(s) {
				signalOwners.recordLocked(s)
			}
		}
		signalOwners.Unlock()
		signal.Notify(sigCh, shutdownSignals()...)
		go handleShutdownSignal(sigCh)
	})
}

// handleShutdownSignal waits for the first signal on sigCh and flushes the SDK.
// It then re-raises the signal if shouldReraise reports that nothing else
// handles it. Otherwise it returns and leaves the process exit to the
// application.
func handleShutdownSignal(sigCh chan os.Signal) {
	sig := <-sigCh

	// Stop listening now so a repeated signal isn't swallowed by our buffered
	// channel but reaches the app or the default disposition, keeping the
	// "press again to force quit" behavior.
	signal.Stop(sigCh)

	logger.Info("received signal, flushing telemetry", "signal", sig.String())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := Shutdown(ctx); err != nil {
		logger.Error("error flushing telemetry during shutdown", "error", err)
	} else {
		logger.Info("OpenTelemetry SDK shutdown completed successfully")
	}

	reraiseIfUnhandled(sig)
}

// signalOwners records which signals the application registered for with
// signal.Notify. The os/signal rules in instrumentation/go.opentelemetry.io/otel/init
// report every signal.Notify call through RecordSignalNotify.
var signalOwners signalRecord

type signalRecord struct {
	sync.Mutex
	// otelcCh is the channel that setupSignalHandler registers.
	otelcCh chan<- os.Signal
	// hookInstalled is true once otelc's own signal.Notify call has gone
	// through RecordSignalNotify, which proves the os/signal instrumentation
	// is applied to this binary.
	hookInstalled bool
	// allSignals is true when the application called signal.Notify with no
	// signal list, which registers for every signal.
	allSignals bool
	// signals holds the signals the application registered for.
	signals map[os.Signal]bool
}

// recordLocked marks sig as handled by the application. The caller must hold r.
func (r *signalRecord) recordLocked(sig os.Signal) {
	if r.signals == nil {
		r.signals = make(map[os.Signal]bool)
	}
	r.signals[sig] = true
}

// RecordSignalNotify is called at the start of every signal.Notify call by the
// os/signal instrumentation, which reaches it through go:linkname. It is not
// meant to be called by application code.
//
// The record is never cleared when the application calls signal.Stop:
// signal.NotifyContext callers often call stop() as soon as the signal arrives
// and then drain for a while, and a re-raise at that point would kill them.
// So an application that registered for a signal at any point keeps today's
// behavior for that signal: otelc flushes and does not re-raise.
func RecordSignalNotify(c chan<- os.Signal, sig []os.Signal) {
	signalOwners.Lock()
	defer signalOwners.Unlock()

	if c != nil && c == signalOwners.otelcCh {
		signalOwners.hookInstalled = true
		return
	}
	if len(sig) == 0 {
		signalOwners.allSignals = true
		return
	}
	for _, s := range sig {
		// signal.Notify ignores signal types it does not know. Skip them
		// too: an uncomparable os.Signal would panic as a map key.
		if s, ok := s.(syscall.Signal); ok {
			signalOwners.recordLocked(s)
		}
	}
}

// reraiseIfUnhandled re-raises sig if shouldReraise allows it. It holds
// signalOwners across the check and the re-raise, so an application that calls
// signal.Notify during the flush either is seen by the check or registers
// after the signal is sent.
func reraiseIfUnhandled(sig os.Signal) {
	signalOwners.Lock()
	defer signalOwners.Unlock()

	if shouldReraise(sig) {
		reraise(sig)
	}
}

// shouldReraise reports whether sig must be re-raised after the flush. That is
// the case only when the os/signal instrumentation is applied and the
// application never registered for sig. Without the instrumentation otelc
// cannot tell whether the application handles sig, so it does not re-raise.
// The caller must hold signalOwners.
func shouldReraise(sig os.Signal) bool {
	return signalOwners.hookInstalled && !signalOwners.allSignals && !signalOwners.signals[sig]
}

// reraise sends sig to the current process. handleShutdownSignal has already
// called signal.Stop, and nothing else registered for sig, so sig now gets the
// disposition it would have without otelc: by default the process terminates
// with the signal. It does nothing on Windows, which cannot send these signals
// to a process.
func reraise(sig os.Signal) {
	p, err := os.FindProcess(os.Getpid())
	if err == nil {
		err = p.Signal(sig)
	}
	if err != nil {
		logger.Debug("could not re-raise signal", "signal", sig.String(), "error", err)
	}
}
