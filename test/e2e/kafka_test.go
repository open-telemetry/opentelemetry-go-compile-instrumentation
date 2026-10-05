//go:build e2e

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/kafka"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"go.opentelemetry.io/otelc/test/testutil"
)

// TestKafka is a true cross-process E2E test that verifies trace context
// propagation between a Kafka producer and consumer binary.
//
// The producer (kafkaproducer) writes a message with an instrumented
// kafka.Writer, which injects a W3C traceparent header into the Kafka message.
// The consumer (kafkaconsumer) reads that same message with an instrumented
// kafka.Reader, which extracts the header and links its span to the producer's
// trace. Both processes export to the same in-process OTLP collector, so a
// single trace with two spans (one Producer, one Consumer) is produced.
func TestKafka(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("kafka testcontainer not supported on windows")
	}
	testcontainers.SkipIfProviderIsNotHealthy(t)

	brokers := startKafkaContainer(t)
	brokerAddrs := strings.Join(brokers, ",")

	// Single fixture so both processes export to the same collector — this
	// allows cross-span trace correlation assertions below.
	f := testutil.NewTestFixture(t)
	f.SetEnv("KAFKA_BROKERS", brokerAddrs)

	// Build both binaries (instrumented by otelc) and run them sequentially on
	// the same topic so the producer's traceparent header lands in the message
	// that the consumer reads.
	producerOut := f.BuildAndRun("kafkaproducer", "-topic=e2e-kafka")
	require.Contains(t, producerOut, "produced message")

	consumerOut := f.BuildAndRun("kafkaconsumer", "-topic=e2e-kafka", "-seed=false")
	require.Contains(t, consumerOut, "consumed message")

	// --- Trace-level correlation ---
	// Both spans must share the same trace ID, proving that the W3C traceparent
	// header injected by the producer was correctly extracted by the consumer.
	f.RequireTraceCount(1)    // one distributed trace
	f.RequireSpansPerTrace(2) // producer span + consumer span

	traces := f.Traces()

	// --- Producer span ---
	producerSpan := testutil.RequireSpan(t, traces,
		func(s ptrace.Span) bool { return s.Kind() == ptrace.SpanKindProducer },
	)
	require.Equal(t, "e2e-kafka send", producerSpan.Name())
	require.NotEqual(t, ptrace.StatusCodeError, producerSpan.Status().Code())

	pAttrs := testutil.Attrs(producerSpan)
	require.Equal(t, "kafka", pAttrs["messaging.system"])
	require.Equal(t, "e2e-kafka", pAttrs["messaging.destination.name"])
	require.Equal(t, "send", pAttrs["messaging.operation.name"])
	require.Equal(t, "send", pAttrs["messaging.operation.type"])

	// --- Consumer span ---
	consumerSpan := testutil.RequireSpan(t, traces,
		func(s ptrace.Span) bool { return s.Kind() == ptrace.SpanKindConsumer },
	)
	require.Equal(t, "e2e-kafka receive", consumerSpan.Name())
	require.NotEqual(t, ptrace.StatusCodeError, consumerSpan.Status().Code())

	cAttrs := testutil.Attrs(consumerSpan)
	require.Equal(t, "kafka", cAttrs["messaging.system"])
	require.Equal(t, "e2e-kafka", cAttrs["messaging.destination.name"])
	require.Equal(t, "receive", cAttrs["messaging.operation.name"])
	require.Equal(t, "receive", cAttrs["messaging.operation.type"])
}

func startKafkaContainer(t *testing.T) []string {
	ctx := t.Context()
	kafkaContainer, err := kafka.Run(ctx, "confluentinc/confluent-local:7.5.0")
	testcontainers.CleanupContainer(t, kafkaContainer)
	require.NoError(t, err)

	brokers, err := kafkaContainer.Brokers(ctx)
	require.NoError(t, err)
	return brokers
}
