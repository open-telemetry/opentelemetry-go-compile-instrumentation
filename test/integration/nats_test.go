// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"go.opentelemetry.io/otelc/test/testutil"
)

func TestNATSClient(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "natsclient", "go", "build", "-a")

	t.Run("Basic", func(t *testing.T) {
		natsURL := startNATSServer(t)

		f := testutil.NewTestFixture(t)
		frontPort := testutil.FreePort(t)
		f.Start("natsclient",
			fmt.Sprintf("-nats-url=%s", natsURL),
			fmt.Sprintf("-front-port=%d", frontPort),
		)
		testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", frontPort))

		publishBody := hitEndpoint(t, frontPort, "/publish")
		var published struct {
			Subject string
		}
		require.NoError(t, json.Unmarshal([]byte(publishBody), &published))
		require.Equal(t, "orders.created", published.Subject)

		f.WaitForSpans(2)
		httpSpan := testutil.RequireSpan(
			t,
			f.Traces(),
			testutil.IsServer,
			func(s ptrace.Span) bool { return s.Name() == "GET /publish" },
		)
		producerSpan := testutil.RequireSpan(
			t,
			f.Traces(),
			testutil.IsProducer,
			func(s ptrace.Span) bool { return s.Name() == "orders.created send" },
		)
		require.Equal(t, httpSpan.TraceID(), producerSpan.TraceID())
		require.Equal(t, httpSpan.SpanID(), producerSpan.ParentSpanID())
		testutil.RequireNATSProducerSemconv(t, producerSpan, "orders.created", endpointHostPort(natsURL))

		consumeBody := hitEndpoint(t, frontPort, "/consume")
		require.Contains(t, consumeBody, "received")
		require.Contains(t, consumeBody, fmt.Sprintf("traceparent=00-%s-%s-01", producerSpan.TraceID(), producerSpan.SpanID()))

		f.WaitForSpans(4)
		processSpan := testutil.RequireSpan(
			t,
			f.Traces(),
			func(s ptrace.Span) bool { return s.Kind() == ptrace.SpanKindConsumer },
			func(s ptrace.Span) bool { return s.Name() == "orders.created process" },
		)
		require.Equal(t, producerSpan.TraceID(), processSpan.TraceID())
		require.Equal(t, producerSpan.SpanID(), processSpan.ParentSpanID())
		testutil.RequireNATSConsumerSemconv(t, processSpan, "orders.created", "process", endpointHostPort(natsURL))
	})

	t.Run("B3Propagator", func(t *testing.T) {
		natsURL := startNATSServer(t)

		// The app is configured with the B3 single-header propagator through the
		// same OTEL_PROPAGATORS env var used by other instrumentation E2E tests.
		// The producer must inject the B3 header onto the real nats-server wire,
		// and the consumer must extract it so the process span continues the
		// producer trace. This is the wire-level counterpart of the in-process
		// B3 carrier round-trip covered by the NATS hook unit tests.
		f := testutil.NewTestFixture(t)
		f.SetEnv("OTEL_PROPAGATORS", "b3")
		frontPort := testutil.FreePort(t)
		f.Start("natsclient",
			fmt.Sprintf("-nats-url=%s", natsURL),
			fmt.Sprintf("-front-port=%d", frontPort),
		)
		testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", frontPort))

		publishBody := hitEndpoint(t, frontPort, "/publish")
		var published struct {
			Subject string
		}
		require.NoError(t, json.Unmarshal([]byte(publishBody), &published))
		require.Equal(t, "orders.created", published.Subject)

		f.WaitForSpans(2)
		httpSpan := testutil.RequireSpan(
			t,
			f.Traces(),
			testutil.IsServer,
			func(s ptrace.Span) bool { return s.Name() == "GET /publish" },
		)
		producerSpan := testutil.RequireSpan(
			t,
			f.Traces(),
			testutil.IsProducer,
			func(s ptrace.Span) bool { return s.Name() == "orders.created send" },
		)
		require.Equal(t, httpSpan.TraceID(), producerSpan.TraceID())
		require.Equal(t, httpSpan.SpanID(), producerSpan.ParentSpanID())
		testutil.RequireNATSProducerSemconv(t, producerSpan, "orders.created", endpointHostPort(natsURL))

		consumeBody := hitEndpoint(t, frontPort, "/consume")
		require.Contains(t, consumeBody, "received")
		_, b3Value, _ := strings.Cut(consumeBody, "b3=")
		b3Value = strings.TrimSpace(b3Value)
		require.NotEmpty(t, b3Value,
			"the configured B3 propagator header must survive the NATS wire to the consumer")
		require.NotContains(t, consumeBody, "traceparent=00-",
			"the B3 propagator must not compose a W3C traceparent header")

		f.WaitForSpans(4)
		processSpan := testutil.RequireSpan(
			t,
			f.Traces(),
			func(s ptrace.Span) bool { return s.Kind() == ptrace.SpanKindConsumer },
			func(s ptrace.Span) bool { return s.Name() == "orders.created process" },
		)
		require.Equal(t, producerSpan.TraceID(), processSpan.TraceID())
		require.Equal(t, producerSpan.SpanID(), processSpan.ParentSpanID())
		testutil.RequireNATSConsumerSemconv(t, processSpan, "orders.created", "process", endpointHostPort(natsURL))
	})

	t.Run("CompositePropagator", func(t *testing.T) {
		natsURL := startNATSServer(t)

		// A composite propagator (W3C TraceContext + B3) must inject both
		// header families onto the real wire; the consumer continues the same
		// producer trace either way.
		f := testutil.NewTestFixture(t)
		f.SetEnv("OTEL_PROPAGATORS", "tracecontext,b3")
		frontPort := testutil.FreePort(t)
		f.Start("natsclient",
			fmt.Sprintf("-nats-url=%s", natsURL),
			fmt.Sprintf("-front-port=%d", frontPort),
		)
		testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", frontPort))

		publishBody := hitEndpoint(t, frontPort, "/publish")
		var published struct {
			Subject string
		}
		require.NoError(t, json.Unmarshal([]byte(publishBody), &published))
		require.Equal(t, "orders.created", published.Subject)

		f.WaitForSpans(2)
		httpSpan := testutil.RequireSpan(
			t,
			f.Traces(),
			testutil.IsServer,
			func(s ptrace.Span) bool { return s.Name() == "GET /publish" },
		)
		producerSpan := testutil.RequireSpan(
			t,
			f.Traces(),
			testutil.IsProducer,
			func(s ptrace.Span) bool { return s.Name() == "orders.created send" },
		)
		require.Equal(t, httpSpan.TraceID(), producerSpan.TraceID())
		require.Equal(t, httpSpan.SpanID(), producerSpan.ParentSpanID())

		consumeBody := hitEndpoint(t, frontPort, "/consume")
		require.Contains(t, consumeBody, "received")
		require.Contains(t, consumeBody, "traceparent=00-",
			"the composite propagator must keep the W3C traceparent on the wire")
		_, b3Value, _ := strings.Cut(consumeBody, "b3=")
		require.NotEmpty(t, strings.TrimSpace(b3Value),
			"the composite propagator must also keep the B3 header on the wire")

		f.WaitForSpans(4)
		processSpan := testutil.RequireSpan(
			t,
			f.Traces(),
			func(s ptrace.Span) bool { return s.Kind() == ptrace.SpanKindConsumer },
			func(s ptrace.Span) bool { return s.Name() == "orders.created process" },
		)
		require.Equal(t, producerSpan.TraceID(), processSpan.TraceID())
		require.Equal(t, producerSpan.SpanID(), processSpan.ParentSpanID())
		testutil.RequireNATSConsumerSemconv(t, processSpan, "orders.created", "process", endpointHostPort(natsURL))
	})

	t.Run("Disabled", func(t *testing.T) {
		natsURL := startNATSServer(t)

		f := testutil.NewTestFixture(t)
		frontPort := testutil.FreePort(t)
		f.SetEnv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "nats")
		f.Start("natsclient",
			fmt.Sprintf("-nats-url=%s", natsURL),
			fmt.Sprintf("-front-port=%d", frontPort),
		)
		testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", frontPort))

		hitEndpoint(t, frontPort, "/publish")
		consumeBody := hitEndpoint(t, frontPort, "/consume")
		require.Contains(t, consumeBody, "received")

		f.WaitForSpans(1)
		for _, span := range testutil.AllSpans(f.Traces()) {
			require.NotContains(t, span.Name(), "orders.created")
		}
	})
}

func startNATSServer(t *testing.T) string {
	t.Helper()
	opts := &server.Options{
		Host:   "127.0.0.1",
		Port:   -1,
		NoLog:  true,
		NoSigs: true,
	}
	s, err := server.NewServer(opts)
	require.NoError(t, err)
	go s.Start()
	if !s.ReadyForConnections(10 * time.Second) {
		t.Fatal("nats server did not become ready")
	}
	t.Cleanup(s.Shutdown)
	return s.ClientURL()
}

func endpointHostPort(natsURL string) string {
	return strings.TrimPrefix(natsURL, "nats://")
}
