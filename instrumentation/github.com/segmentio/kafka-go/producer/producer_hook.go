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

	// asyncCompletionOnce tracks, per *kafka.Writer, the sync.Once that guards
	// installing the logging wrapper on Completion (see
	// ensureAsyncFailureLogging), so a writer reused across many WriteMessages
	// calls is only wrapped once. A sync.Once (rather than a plain
	// LoadOrStore-guarded bool) matters here: it makes every concurrent caller
	// for the same writer block until the wrap actually completes, including
	// the ones that lose the race to perform it, which is what keeps the write
	// to w.Completion from racing with kafka-go's own unsynchronized read of
	// that field from a background completion goroutine spawned by a losing
	// caller's WriteMessages call.
	//
	// Entries are keyed by the writer's own pointer and are never removed, so
	// an application that creates short-lived *kafka.Writer values (uncommon,
	// but not forbidden by kafka-go) leaks one entry per writer for the
	// process's lifetime. kafka.Writer values are conventionally constructed
	// once per process and reused for its lifetime, so this trades a
	// theoretical leak for simplicity rather than being an oversight.
	asyncCompletionOnce sync.Map //nolint:gochecknoglobals // per-writer wrap tracking, see comment above
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
// per *kafka.Writer (see asyncCompletionOnce), and every concurrent caller for
// the same writer blocks until that one run has finished, which is required
// for correctness — see asyncCompletionOnce's doc comment.
//
// This assumes Completion is set once, at construction (or at least before
// the writer is shared across goroutines) and not mutated afterward. If
// application code reassigns w.Completion after this has already wrapped it,
// that assignment silently replaces the wrapper installed here with no error
// and no log line: asyncCompletionOnce still shows this writer as wrapped, so
// a later call here won't re-wrap it, and the failure logging this function
// adds just stops happening. This is not detected or guarded against.
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
//
// Note: kafka-go's Writer.Close blocks on Completion callback calls, so the
// extra work done here (the conditional logger.Error call) runs inside that
// blocking window. Keep it cheap.
func ensureAsyncFailureLogging(w *kafka.Writer) {
	onceIface, _ := asyncCompletionOnce.LoadOrStore(w, new(sync.Once))
	once, ok := onceIface.(*sync.Once)
	if !ok {
		return
	}
	once.Do(func() {
		original := w.Completion
		w.Completion = func(msgs []kafka.Message, err error) {
			if err != nil {
				logger.Error("kafka async write failed after WriteMessages returned; the producer span(s) for the affected message(s) were already ended without this outcome",
					"error", err, "messageCount", len(msgs))
			}
			if original != nil {
				original(msgs, err)
			}
		}
	})
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
// returns before that happens, so these spans still end immediately with
// enqueue-time duration and, in the absence of an error, Ok/Unset status —
// that does not change here. This function only labels that fact: the
// messaging.kafka.async attribute (set in BeforeWriteMessages) tells
// consumers of the trace data not to read this span's duration and status as
// confirmed broker delivery. It does not make the span's duration or status
// itself reflect delivery.
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
