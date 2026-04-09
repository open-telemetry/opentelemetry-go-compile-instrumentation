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
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

var (
	logger     = shared.Logger()
	tracer     trace.Tracer
	propagator propagation.TextMapPropagator
	initOnce   sync.Once
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
		propagator = otel.GetTextMapPropagator()

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

	endpoint := ""
	if brokers := r.Config().Brokers; len(brokers) > 0 {
		endpoint = brokers[0]
	}
	ictx.SetData(map[string]interface{}{
		"endpoint":  endpoint,
		"group_id":  r.Config().GroupID,
		"partition": strconv.Itoa(r.Config().Partition),
		"start":     time.Now(),
	})
}

func AfterReadMessage(ictx inst.HookContext, ctx context.Context, msg kafka.Message, err error) {
	if !kafkaEnabler.Enable() {
		return
	}

	data, ok := ictx.GetData().(map[string]interface{})
	if !ok {
		return
	}

	startTime, _ := data["start"].(time.Time)

	if err != nil {
		_, span := tracer.Start(ctx, "kafka receive",
			trace.WithSpanKind(trace.SpanKindConsumer),
			trace.WithTimestamp(startTime),
		)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.End()
		logger.Debug("AfterReadMessage called with error", err)
		return
	}

	// Process message and extract its Metadata
	req := semconv.KafkaRequest{
		EndPoint:        data["endpoint"].(string),
		Destination:     semconv.KafkaDestination(msg.Topic),
		Operation:       semconv.KafkaOperationReceive,
		ConsumerGroupID: data["group_id"].(string),
		Partition:       data["partition"].(string),
	}

	carrier := KafkaHeaderCarrier{headers: &msg.Headers}
	extractCtx := propagator.Extract(ctx, carrier)

	newCtx, span := tracer.Start(
		extractCtx,
		msg.Topic+" receive",
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(semconv.KafkaRequestTraceAttrs(req)...),
		trace.WithTimestamp(startTime),
	)

	data["ctx"] = newCtx
	data["span"] = span
	logger.Debug("AfterReadMessage Called",
		"duration_ms", time.Since(startTime).Milliseconds())
}

func AfterMessageProcessing(ictx inst.HookContext, err error) {
	if !kafkaEnabler.Enable() {
		return
	}
	data, ok := ictx.GetData().(map[string]interface{})
	// Check for any errors
	if !ok {
		return
	}

	span, ok := data["span"].(trace.Span)
	if !ok || span == nil {
		return
	}

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}

	// end the Span here
	defer span.End()
}
