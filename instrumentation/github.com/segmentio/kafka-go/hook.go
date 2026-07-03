// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package kafka

import (
	"context"
	"errors"
	"sync"
	"time"

	kafka "github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/github.com/segmentio/kafka-go/semconv"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/hook"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/runtime"
)

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
		logger.Info("Kafka (segmentio/kafka-go) instrumentation initialized")
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
			MessageKey:      string(msgs[i].Key),
			MessageBodySize: len(msgs[i].Value),
		}
		msgCtx, span := tracer.Start(ctx, topic+" send",
			trace.WithSpanKind(trace.SpanKindProducer),
			trace.WithAttributes(semconv.KafkaRequestTraceAttrs(req)...),
		)
		propagator.Inject(msgCtx, headerCarrier{headers: &msgs[i].Headers})
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

// -----------------------------------------------------------------------------
// Consumer: (*kafka.Reader).ReadMessage(ctx)
// -----------------------------------------------------------------------------

type consumerData struct {
	ctx      context.Context
	endpoint string
	topic    string
	groupID  string
	start    time.Time
}

// BeforeReadMessage captures the reader configuration and the call start time so
// AfterReadMessage can build an accurate consumer span once the message arrives.
func BeforeReadMessage(ictx hook.HookContext, r *kafka.Reader, ctx context.Context) {
	if !kafkaEnabler.Enable() {
		logger.Debug("Kafka consumer instrumentation disabled")
		return
	}
	if r == nil {
		return
	}
	initInstrumentation()

	cfg := r.Config()
	endpoint := ""
	if len(cfg.Brokers) > 0 {
		endpoint = cfg.Brokers[0]
	}
	ictx.SetData(&consumerData{
		ctx:      ctx,
		endpoint: endpoint,
		topic:    cfg.Topic,
		groupID:  cfg.GroupID,
		start:    time.Now(),
	})
}

// AfterReadMessage creates a consumer span that links to the producer via the
// trace context carried in the Kafka message headers.
//
// The Enable() check is intentionally omitted: if BeforeReadMessage was
// disabled, no consumerData was stored, so GetData returns nil and we return
// early anyway. Re-checking here could skip span.End() if the flag flipped
// between Before and After, leaking spans whose context was already injected.
func AfterReadMessage(ictx hook.HookContext, msg kafka.Message, err error) {
	data, ok := ictx.GetData().(*consumerData)
	if !ok || data == nil {
		return
	}

	topic := msg.Topic
	if topic == "" {
		topic = data.topic
	}

	parent := data.ctx
	if parent == nil {
		parent = context.Background()
	}
	parent = propagator.Extract(parent, headerCarrier{headers: &msg.Headers})

	req := semconv.KafkaRequest{
		Endpoint:        data.endpoint,
		Destination:     topic,
		Operation:       semconv.KafkaOperationReceive,
		ConsumerGroupID: data.groupID,
		MessageKey:      string(msg.Key),
		MessageBodySize: len(msg.Value),
		Partition:       msg.Partition,
		Offset:          msg.Offset,
		HasPartition:    err == nil,
		HasOffset:       err == nil,
	}
	_, span := tracer.Start(parent, topic+" receive",
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithTimestamp(data.start),
		trace.WithAttributes(semconv.KafkaRequestTraceAttrs(req)...),
	)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}

// ExtractContext extracts the trace context from a Kafka message's headers
// and returns a context.Context that carries the propagated span context.
//
// Use this with the message returned by (*kafka.Reader).ReadMessage to
// continue the trace in downstream message-processing code:
//
//	msg, err := r.ReadMessage(ctx)
//	ctx = kafka.ExtractContext(msg)
//	// spans created with ctx will be children of the producer span.
func ExtractContext(msg kafka.Message) context.Context {
	initInstrumentation()
	return propagator.Extract(context.Background(), headerCarrier{headers: &msg.Headers})
}
