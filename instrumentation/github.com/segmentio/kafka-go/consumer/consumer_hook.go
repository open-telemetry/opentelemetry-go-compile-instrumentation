// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package consumer

import (
	"context"
	"sync"
	"time"

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

// kafkaEnablerImpl controls whether the kafka-go consumer instrumentation is enabled.
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
		logger.Info("Kafka (segmentio/kafka-go) consumer instrumentation initialized")
	})
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

// fetchMessageCallKey marks a context as coming from (*kafka.Reader).ReadMessage's
// own call into r.FetchMessage(ctx) (see kafka-go's reader.go), so
// BeforeFetchMessage can recognize and skip that nested call: ReadMessage is
// already instrumented, and without this the same message would get a second,
// duplicate consumer span from the inner FetchMessage call.
type fetchMessageCallKey struct{}

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

	// ReadMessage's own body calls r.FetchMessage(ctx) with this same ctx value,
	// so marking it here and writing it back via SetParam lets BeforeFetchMessage
	// recognize that nested call and skip creating a duplicate span for it.
	// context.WithValue panics on a nil parent, so normalize first.
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = context.WithValue(ctx, fetchMessageCallKey{}, struct{}{})
	ictx.SetParam(1, ctx)

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
	parent = propagator.Extract(parent, kafkaprop.NewHeaderCarrier(&msg.Headers))

	req := semconv.KafkaRequest{
		Endpoint:        data.endpoint,
		Destination:     topic,
		Operation:       semconv.KafkaOperationReceive,
		ConsumerGroupID: data.groupID,
		MessageKey:      semconv.KafkaMessageKey(msg.Key),
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

// BeforeFetchMessage captures the reader configuration and the call start time
// so AfterFetchMessage can build an accurate consumer span once the message
// arrives. FetchMessage does not auto-commit the offset; the caller must
// explicitly call CommitMessages after processing.
//
// (*kafka.Reader).ReadMessage's own implementation calls r.FetchMessage(ctx)
// internally, and both methods are instrumented (see otelc.yaml), so a call to
// ReadMessage would otherwise also trigger this hook for its nested FetchMessage
// call, producing two consumer spans for one message. When ctx carries the
// marker BeforeReadMessage set, this is that nested call, so it's skipped here:
// no data is stored, and AfterFetchMessage's delegation to AfterReadMessage
// already no-ops on missing data (see its comment).
func BeforeFetchMessage(ictx hook.HookContext, r *kafka.Reader, ctx context.Context) {
	if ctx != nil && ctx.Value(fetchMessageCallKey{}) != nil {
		return
	}
	BeforeReadMessage(ictx, r, ctx)
}

// AfterFetchMessage creates a consumer span for a message received via
// FetchMessage. It delegates to AfterReadMessage because the two methods share
// the same parameter layout and span semantics.
//
// The Enable() check is intentionally omitted — see AfterReadMessage.
func AfterFetchMessage(ictx hook.HookContext, msg kafka.Message, err error) {
	AfterReadMessage(ictx, msg, err)
}

// ExtractContext extracts the trace context from a Kafka message's headers
// and returns a context.Context that carries the propagated span context.
//
// Use this with the message returned by (*kafka.Reader).ReadMessage or
// (*kafka.Reader).FetchMessage to continue the trace in downstream
// message-processing code:
//
//	msg, err := r.FetchMessage(ctx)
//	ctx = consumer.ExtractContext(msg)
//	// spans created with ctx will be children of the producer span.
func ExtractContext(msg kafka.Message) context.Context {
	initInstrumentation()
	return propagator.Extract(context.Background(), kafkaprop.NewHeaderCarrier(&msg.Headers))
}
