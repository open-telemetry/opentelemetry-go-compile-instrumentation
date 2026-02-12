package slog

import (
	logslog "log/slog"
	"runtime/debug"
	"sync/atomic"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/shared"
	"go.opentelemetry.io/contrib/bridges/otelslog"
)

const (
	instrumentationName = "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/log"
	instrumentationKey  = "LOG"
)

var (
	logger           = shared.Logger()
	initializerStart = new(atomic.Bool)
	handlerProvier   = new(atomic.Value)
)

// moduleVersion extracts the version from the Go module system.
// Falls back to "dev" if version cannot be determined.
func moduleVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}

	// Return the main module version
	if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}

	return "dev"
}

func initInstrumentation() {
	// Avoid sync.Once here.
	// initInstrumentation logs via slog, which re-enters initInstrumentation.
	// A re-entrant call would block on sync.Once and cause a deadlock.
	if initializerStart.CompareAndSwap(false, true) {
		version := moduleVersion()
		if err := shared.SetupOTelSDK("go.opentelemetry.io/compile-instrumentation/log/slog", version); err != nil {
			logger.Error("failed to setup OTel SDK", "error", err)
		}

		handlerProvier.Store(otelslog.NewHandler(instrumentationName))

		// Start runtime metrics (respects OTEL_GO_ENABLED/DISABLED_INSTRUMENTATIONS)
		if err := shared.StartRuntimeMetrics(); err != nil {
			logger.Error("failed to start runtime metrics", "error", err)
		}

		logger.Info("slog logger instrumentation initialized")
	}
}

// slogLoggerEnabler controls whether server instrumentation is enabled
type slogLoggerEnabler struct{}

func (z slogLoggerEnabler) Enable() bool {
	return shared.Instrumented(instrumentationKey)
}

var loggerEnabler = slogLoggerEnabler{}

func BeforeHandler(ictx inst.HookContext, recv *logslog.Logger) {
	if !loggerEnabler.Enable() {
		logger.Debug("slog logger instrumentation disabled")
		return
	}

	initInstrumentation()

	if handlerProvier.Load() != nil {
		recv.WrapHandler(wrapperFunc)
	}
}

func BeforeWith(ictx inst.HookContext, recv *logslog.Logger, _ ...any) {
	if !loggerEnabler.Enable() {
		logger.Debug("slog logger instrumentation disabled")
		return
	}

	initInstrumentation()

	if handlerProvier.Load() != nil {
		recv.WrapHandler(wrapperFunc)
	}
}

func BeforeWithGroup(ictx inst.HookContext, recv *logslog.Logger, _ string) {
	if !loggerEnabler.Enable() {
		logger.Debug("slog logger instrumentation disabled")
		return
	}

	initInstrumentation()

	if handlerProvier.Load() != nil {
		recv.WrapHandler(wrapperFunc)
	}
}

func wrapperFunc(h logslog.Handler) logslog.Handler {
	if _, ok := h.(*HandlerWrapper); ok {
		return h
	}

	return &HandlerWrapper{
		Handler:     h,
		otelHandler: handlerProvier.Load().(logslog.Handler),
	}
}
