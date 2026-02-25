// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package zap

import (
	"runtime/debug"
	"sync"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/shared"
	"go.opentelemetry.io/contrib/bridges/otelzap"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	instrumentationName = "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/log"
	instrumentationKey  = "LOG"
)

var (
	logger   = shared.Logger()
	core     zapcore.Core
	initOnce sync.Once
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
	initOnce.Do(func() {
		version := moduleVersion()
		if err := shared.SetupOTelSDK("go.opentelemetry.io/compile-instrumentation/log/zap", version); err != nil {
			logger.Error("failed to setup OTel SDK", "error", err)
		}

		core = otelzap.NewCore(instrumentationName)

		// Start runtime metrics (respects OTEL_GO_ENABLED/DISABLED_INSTRUMENTATIONS)
		if err := shared.StartRuntimeMetrics(); err != nil {
			logger.Error("failed to start runtime metrics", "error", err)
		}

		logger.Info("zap logger instrumentation initialized")
	})
}

// zapLoggerEnabler controls whether server instrumentation is enabled
type zapLoggerEnabler struct{}

func (z zapLoggerEnabler) Enable() bool {
	return shared.Instrumented(instrumentationKey)
}

var loggerEnabler = zapLoggerEnabler{}

func BeforeWrite(ictx inst.HookContext, recv *zapcore.CheckedEntry, fields ...zap.Field) {
	if !loggerEnabler.Enable() {
		logger.Debug("zap logger instrumentation disabled")
		return
	}

	initInstrumentation()

	recv.AddCore(recv.Entry, core)
}
