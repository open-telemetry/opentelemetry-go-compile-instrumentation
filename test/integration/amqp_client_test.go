// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/rabbitmq"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"go.opentelemetry.io/otelc/test/testutil"
)

func TestAmqpClient(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("rabbitmq testcontainer not supported on windows")
	}
	testcontainers.SkipIfProviderIsNotHealthy(t)

	t.Parallel()
	// One Build for all cases. Parallel top-level tests that each Build
	// amqpclient race on the same ./app and .otelc-build; the first
	// Cleanup deletes them.
	testutil.Build(t, "", "amqpclient", "go", "build", "-a")

	amqpURL := startRabbitMQ(t)

	t.Run("manual_ack", func(t *testing.T) {
		f := testutil.NewTestFixture(t)
		out := f.Run("amqpclient", "-amqp-url="+amqpURL, "-queue=orders")
		require.Contains(t, out, "published message")
		require.Contains(t, out, "consumed message")

		spans := testutil.AllSpans(f.Traces())
		require.Len(t, spans, 2)

		send := testutil.RequireSpan(t, f.Traces(),
			func(s ptrace.Span) bool { return s.Kind() == ptrace.SpanKindProducer },
		)
		require.Equal(t, "(default) send", send.Name())
		require.NotEqual(t, ptrace.StatusCodeError, send.Status().Code())
		testutil.RequireAttribute(t, send, "messaging.system", "rabbitmq")
		testutil.RequireAttribute(t, send, "messaging.operation.name", "send")
		testutil.RequireAttribute(t, send, "messaging.operation.type", "send")
		testutil.RequireAttribute(t, send, "messaging.destination.name", "(default)")
		testutil.RequireAttribute(t, send, "messaging.rabbitmq.destination.routing_key", "orders")

		process := testutil.RequireSpan(t, f.Traces(),
			func(s ptrace.Span) bool { return s.Kind() == ptrace.SpanKindConsumer },
		)
		require.Equal(t, "orders process", process.Name())
		require.NotEqual(t, ptrace.StatusCodeError, process.Status().Code())
		testutil.RequireAttribute(t, process, "messaging.system", "rabbitmq")
		testutil.RequireAttribute(t, process, "messaging.operation.name", "process")
		testutil.RequireAttribute(t, process, "messaging.operation.type", "process")
		testutil.RequireAttribute(t, process, "messaging.destination.name", "orders")
		testutil.RequireAttribute(t, process, "messaging.destination.subscription.name", "orders")

		require.Equal(t, send.TraceID(), process.TraceID())
		require.Equal(t, send.SpanID(), process.ParentSpanID())
	})

	t.Run("auto_ack", func(t *testing.T) {
		f := testutil.NewTestFixture(t)
		out := f.Run("amqpclient", "-amqp-url="+amqpURL, "-queue=autoack", "-auto-ack")
		require.Contains(t, out, "published message")
		require.Contains(t, out, "consumed message")

		spans := testutil.AllSpans(f.Traces())
		require.Len(t, spans, 2)

		receive := testutil.RequireSpan(t, f.Traces(),
			func(s ptrace.Span) bool { return s.Kind() == ptrace.SpanKindConsumer },
		)
		require.Equal(t, "autoack receive", receive.Name())
		require.NotEqual(t, ptrace.StatusCodeError, receive.Status().Code())
		testutil.RequireAttribute(t, receive, "messaging.operation.name", "receive")
		testutil.RequireAttribute(t, receive, "messaging.operation.type", "receive")
	})

	t.Run("disabled", func(t *testing.T) {
		f := testutil.NewTestFixture(t)
		f.SetEnv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "amqp")
		out := f.Run("amqpclient", "-amqp-url="+amqpURL, "-queue=disabled")
		require.Contains(t, out, "published message")
		require.Contains(t, out, "consumed message")
		require.Empty(t, testutil.AllSpans(f.Traces()))
	})
}

func startRabbitMQ(t *testing.T) string {
	t.Helper()
	ctr, err := rabbitmq.Run(t.Context(), "rabbitmq:3.13-alpine")
	require.NoError(t, err)
	testcontainers.CleanupContainer(t, ctr)
	url, err := ctr.AmqpURL(t.Context())
	require.NoError(t, err)
	return url
}
