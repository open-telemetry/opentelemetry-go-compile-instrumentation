// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package segmentio

import (
	"context"
	"net"
	"runtime/debug"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"
	"unsafe"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/shared"
	"github.com/segmentio/kafka-go"
	"github.com/tushar1977/opentelemetry-go-compile-instrumentation/pkg/instrumentation/kafka/semconv"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var (
	logger   = shared.Logger()
	tracer   trace.Tracer
	initOnce sync.Once
)

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
		if err := shared.SetupOTelSDK("go.opentelemetry.io/compile-instrumentation/kafka/segmentio", version); err != nil {
			logger.Error("failed to setup OTel SDK", "error", err)
		}
		tracer = otel.GetTracerProvider().Tracer(
			instrumentationName,
			trace.WithInstrumentationVersion(version),
		)

		// Start runtime metrics (respects OTEL_GO_ENABLED/DISABLED_INSTRUMENTATIONS)
		if err := shared.StartRuntimeMetrics(); err != nil {
			logger.Error("failed to start runtime metrics", "error", err)
		}

		logger.Info("Kafka Segmentio client instrumentation initialized")
	})
}

type Reader struct {
	*kafka.Reader
}

func NewReader(config kafka.ReaderConfig) *Reader {
	initInstrumentation()
	return &Reader{Reader: kafka.NewReader(config)}
}

func (c *Reader) ReadMessage(ctx context.Context) (kafka.Message, error) {
	if !kafkaEnabler.Enable() {
		logger.Debug("Kafka Client instrumentation disabled")
		return c.Reader.ReadMessage(ctx)
	}
	req := semconv.Kafka
}
