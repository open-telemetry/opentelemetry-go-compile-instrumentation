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

// headerCarrier adapts a slice of kafka.Header to the OpenTelemetry
// TextMapCarrier interface so trace context can be propagated through Kafka
// message headers.
type headerCarrier struct {
	headers *[]kafka.Header
}

// Get returns the value of the first header matching key, or "" if absent.
func (c headerCarrier) Get(key string) string {
	for _, h := range *c.headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}

// Set replaces any existing header with key, otherwise appends a new one.
func (c headerCarrier) Set(key, value string) {
	for i := range *c.headers {
		if (*c.headers)[i].Key == key {
			(*c.headers)[i].Value = []byte(value)
			return
		}
	}
	*c.headers = append(*c.headers, kafka.Header{Key: key, Value: []byte(value)})
}

// Keys lists the header keys carried by this carrier.
func (c headerCarrier) Keys() []string {
	keys := make([]string, 0, len(*c.headers))
	for _, h := range *c.headers {
		keys = append(keys, h.Key)
	}
	return keys
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
//	ctx = consumer.ExtractContext(msg)
//	// spans created with ctx will be children of the producer span.
func ExtractContext(msg kafka.Message) context.Context {
	initInstrumentation()
	return propagator.Extract(context.Background(), headerCarrier{headers: &msg.Headers})
}
