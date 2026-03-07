// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package segmentio

import (
	"context"
	"runtime/debug"
	"strconv"
	"sync"
	"time"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/kafka/semconv"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/shared"
	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
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

func BeforeReadMessage(ictx inst.HookContext, ctx context.Context, r *kafka.Reader) {
	if !kafkaEnabler.Enable() {
		logger.Debug("Kafka Client instrumentation disabled")
		return
	}
	initInstrumentation()

	req := semconv.KafkaRequest{
		EndPoint:        r.Config().Brokers[0],
		Destination:     semconv.KafkaDestinationTopic,
		Operation:       semconv.KafkaOperationReceive,
		ConsumerGroupID: r.Config().GroupID,
		Partition:       strconv.Itoa(r.Config().Partition),
	}

	attrs := semconv.KafkaRequestTraceAttrs(req)

	spanName := r.Config().Topic + " " + "receive"

	prop := otel.GetTextMapPropagator()
	carrier := propagation.HeaderCarrier{}

	extractCtx := prop.Extract(ctx, &carrier)

	newctx, span := tracer.Start(extractCtx, spanName, trace.WithAttributes(attrs...), trace.WithSpanKind(trace.SpanKindConsumer))

	ictx.SetData(map[string]interface{}{
		"ctx":   newctx,
		"span":  span,
		"req":   req,
		"start": time.Now(),
	})

	ictx.SetParam(1, newctx)
}
