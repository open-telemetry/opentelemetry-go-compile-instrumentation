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

	// asyncCompletionWrapped tracks which *kafka.Writer instances already have
	// a logging wrapper installed on Completion (see ensureAsyncFailureLogging),
	// so a writer reused across many WriteMessages calls is only wrapped once.
	// Entries are keyed by the writer's own pointer and are never removed, but
	// kafka.Writer values are conventionally constructed once per process and
	// reused for its lifetime, not created per call, so this stays bounded by
	// the number of distinct writers an application creates, not by traffic.
	asyncCompletionWrapped sync.Map //nolint:gochecknoglobals // per-writer wrap tracking, see comment above
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

	if w.Async {
		// WriteMessages returns as soon as messages are enqueued, before the
		// broker write happens, so any failure only ever reaches the caller
		// through Completion — long after the spans below have already been
		// ended by AfterWriteMessages. Without this, such failures are
		// completely invisible: not on the span, not anywhere. Wrapping
		// Completion at least surfaces them in the logs.
		ensureAsyncFailureLogging(w)
	}

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
			Async:           w.Async,
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

// ensureAsyncFailureLogging installs a wrapper around w.Completion, preserving
// any callback the caller already configured, so that write failures reported
// asynchronously for an Async writer are at least logged. It runs at most once
// per *kafka.Writer (see asyncCompletionWrapped).
//
// This does not attempt to correlate a failure back to the specific span(s)
// for the affected message(s): by the time Completion fires, those spans have
// already been ended by AfterWriteMessages (WriteMessages returns before the
// broker write happens for an Async writer, so there is no later hook to defer
// span-ending to). Reliable correlation would require either depending on a
// configured propagator (silently finding nothing when one isn't set, which is
// the OTel default) or attaching a tracking header to every outgoing message
// that would then be visible to real Kafka consumers — both worse than not
// correlating. Logging the failure is the honest, dependency-free improvement
// over the previous behavior, where such failures were entirely invisible.
func ensureAsyncFailureLogging(w *kafka.Writer) {
	if _, alreadyWrapped := asyncCompletionWrapped.LoadOrStore(w, struct{}{}); alreadyWrapped {
		return
	}
	original := w.Completion
	w.Completion = func(msgs []kafka.Message, err error) {
		if err != nil {
			//nolint:sloglint // matches the existing package-level logger usage in this file
			logger.Error("kafka async write failed after WriteMessages returned; "+
				"the producer span(s) for the affected message(s) were already ended without this outcome",
				"error", err, "messageCount", len(msgs))
		}
		if original != nil {
			original(msgs, err)
		}
	}
}

// AfterWriteMessages finalizes the producer spans created by BeforeWriteMessages.
//
// kafka.WriteMessages may return kafka.WriteErrors — a []error aligned with
// the message slice — to indicate partial success. When that happens, only the
// spans for messages whose entry is non-nil are marked as Error; the rest stay
// Ok. For any other error type, the error is applied to every span.
//
// For an Async writer, a nil err here only means the messages were handed off
// to the client library, not that the broker write succeeded: WriteMessages
// returns before that happens. These spans are marked via the
// messaging.kafka.async attribute (set in BeforeWriteMessages) so their
// duration and Unset/Ok status aren't misread as confirmed delivery.
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
