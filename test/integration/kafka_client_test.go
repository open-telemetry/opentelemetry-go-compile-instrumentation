// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/kafka"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/testutil"
)

func TestKafkaClient(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("kafka testcontainer not supported on windows")
	}

	t.Parallel()
	testutil.Build(t, "", "kafkaclient", "go", "build", "-a")

	brokers := startKafkaContainer(t)

	t.Run("Produce", func(t *testing.T) {
		f := testutil.NewTestFixture(t)
		f.SetEnv("KAFKA_BROKERS", strings.Join(brokers, ","))

		out := f.Run("kafkaclient", "-op=produce", "-topic=orders")
		require.Contains(t, out, "produced message")

		span := f.RequireSingleSpan()
		require.Equal(t, "orders send", span.Name())
		require.Equal(t, ptrace.SpanKindProducer, span.Kind())

		attrs := testutil.Attrs(span)
		require.Equal(t, "kafka", attrs["messaging.system"])
		require.Equal(t, "send", attrs["messaging.operation.name"])
		require.Equal(t, "send", attrs["messaging.operation.type"])
		require.Equal(t, "orders", attrs["messaging.destination.name"])
		require.Equal(t, "order-1", attrs["messaging.kafka.message.key"])
		require.NotEmpty(t, attrs["server.address"])
	})

	t.Run("Consume", func(t *testing.T) {
		f := testutil.NewTestFixture(t)
		f.SetEnv("KAFKA_BROKERS", strings.Join(brokers, ","))

		// The consumer seeds a message and reads it back from the real broker,
		// so the injected hook records a successful consumer span with the
		// resolved partition and offset.
		out := f.Run("kafkaclient", "-op=consume", "-topic=orders")
		require.Contains(t, out, "consumed message")

		span := testutil.RequireSpan(t, f.Traces(),
			func(s ptrace.Span) bool { return s.Kind() == ptrace.SpanKindConsumer },
		)
		require.Equal(t, "orders receive", span.Name())
		require.NotEqual(t, ptrace.StatusCodeError, span.Status().Code())

		attrs := testutil.Attrs(span)
		require.Equal(t, "kafka", attrs["messaging.system"])
		require.Equal(t, "receive", attrs["messaging.operation.name"])
		require.Equal(t, "receive", attrs["messaging.operation.type"])
		require.Equal(t, "orders", attrs["messaging.destination.name"])
		require.Equal(t, "0", attrs["messaging.destination.partition.id"])
		require.Contains(t, attrs, "messaging.kafka.offset")
	})
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
