// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package producer

import (
	"context"
	"errors"
	"sync"

	kafka "github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	kafkaprop "go.opentelemetry.io/otelc/instrumentation/github.com/segmentio/kafka-go/internal/propagation"
	"go.opentelemetry.io/otelc/instrumentation/github.com/segmentio/kafka-go/semconv"
	"go.opentelemetry.io/otelc/pkg/hook"
	"go.opentelemetry.io/otelc/pkg/runtime"
)

const (
	instrumentationName = "go.opentelemetry.io/otelc/" +
		"instrumentation/github.com/segmentio/kafka-go"
	instrumentationKey = "KAFKA"
)

// kafkaEnablerImpl controls whether the kafka-go producer instrumentation is enabled.
type kafkaEnablerImpl struct{}

func (kafkaEnablerImpl) Enable() bool {
	return runtime.Instrumented(instrumentationKey)
}

var kafkaEnabler = kafkaEnablerImpl{}

var (
	logger     = runtime.Logger()
	tracer     trace.Tracer
	propagator propagation.TextMapPropagator
	initOnce   sync.Once
)

func initInstrumentation() {
	initOnce.Do(func() {
		tracer = otel.GetTracerProvider().Tracer(
			instrumentationName,
			trace.WithInstrumentationVersion(runtime.ModuleVersion()),
		)
		propagator = otel.GetTextMapPropagator()
		logger.Info("Kafka (segmentio/kafka-go) producer instrumentation initialized")
	})
}

// -----------------------------------------------------------------------------
// Producer: (*kafka.Writer).WriteMessages(ctx, msgs...)
// -----------------------------------------------------------------------------

// BeforeWriteMessages starts a producer span per message, injects the trace
// context into each message's headers and hands the (possibly modified) message
// slice back to the original call so the propagated headers are actually sent.
func BeforeWriteMessages(
	ictx hook.HookContext,
	w *kafka.Writer,
	ctx context.Context,
	msgs ...kafka.Message,
) {
	if !kafkaEnabler.Enable() {
		logger.Debug("Kafka producer instrumentation disabled")
		return
	}
	if w == nil || len(msgs) == 0 {
		return
	}
	initInstrumentation()

	endpoint := ""
	if w.Addr != nil {
		endpoint = w.Addr.String()
	}

	spans := make([]trace.Span, len(msgs))
	for i := range msgs {
		topic := msgs[i].Topic
		if topic == "" {
			topic = w.Topic
		}
		req := semconv.KafkaRequest{
			Endpoint:        endpoint,
			Destination:     topic,
			Operation:       semconv.KafkaOperationSend,
			MessageKey:      semconv.KafkaMessageKey(msgs[i].Key),
			MessageBodySize: len(msgs[i].Value),
		}
		msgCtx, span := tracer.Start(ctx, topic+" send",
			trace.WithSpanKind(trace.SpanKindProducer),
			trace.WithAttributes(semconv.KafkaRequestTraceAttrs(req)...),
		)
		propagator.Inject(msgCtx, kafkaprop.NewHeaderCarrier(&msgs[i].Headers))
		spans[i] = span
	}

	// Propagate the header-injected messages to the real WriteMessages call.
	ictx.SetParam(2, msgs)
	ictx.SetData(spans)
}

// AfterWriteMessages finalizes the producer spans created by BeforeWriteMessages.
//
// kafka.WriteMessages may return kafka.WriteErrors — a []error aligned with
// the message slice — to indicate partial success. When that happens, only the
// spans for messages whose entry is non-nil are marked as Error; the rest stay
// Ok. For any other error type, the error is applied to every span.
func AfterWriteMessages(ictx hook.HookContext, err error) {
	spans, ok := ictx.GetData().([]trace.Span)
	if !ok {
		return
	}

	var writeErrs kafka.WriteErrors
	isWriteErrors := errors.As(err, &writeErrs)

	for i, span := range spans {
		if span == nil {
			continue
		}
		if isWriteErrors {
			// Partial failure: only mark the spans for messages that actually
			// failed (index-aligned with writeErrs).
			if i < len(writeErrs) && writeErrs[i] != nil {
				span.RecordError(writeErrs[i])
				span.SetStatus(codes.Error, writeErrs[i].Error())
			}
		} else if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}
}
