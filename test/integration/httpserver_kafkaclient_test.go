// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"go.opentelemetry.io/otelc/test/testutil"
)

func TestHTTPServerKafkaClient(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("kafka testcontainer not supported on windows")
	}
	testcontainers.SkipIfProviderIsNotHealthy(t)

	// Not using t.Parallel() here to prevent CI resource exhaustion 
	// from spinning up multiple heavy Kafka testcontainers simultaneously.
	testutil.Build(t, "", "httpserverkafkaclient", "go", "build", "-a")

	brokers := startKafkaContainer(t)

	f := testutil.NewTestFixture(t)
	f.SetEnv("KAFKA_BROKERS", strings.Join(brokers, ","))

	frontPort := testutil.FreePort(t)
	f.Start("httpserverkafkaclient", fmt.Sprintf("-front-port=%d", frontPort))
	testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", frontPort))

	// Send request to frontend
	url := fmt.Sprintf("http://127.0.0.1:%d/produce", frontPort)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Wait for the spans to be flushed
	testutil.WaitForSpanFlush(t)

	// We expect exactly 1 trace with 2 spans:
	// 1. HTTP server (Frontend)
	// 2. Kafka producer (Frontend -> Broker)
	f.RequireTraceCount(1)
	f.RequireSpansPerTrace(2)

	httpServerSpan := testutil.RequireSpan(
		t,
		f.Traces(),
		testutil.IsServer,
		func(s ptrace.Span) bool { return s.Name() == "GET" },
	)
	kafkaProducerSpan := testutil.RequireSpan(
		t,
		f.Traces(),
		testutil.IsProducer,
		func(s ptrace.Span) bool { return s.Name() == "test-topic-http send" },
	)

	attrs := testutil.Attrs(kafkaProducerSpan)
	require.Equal(t, "kafka", attrs["messaging.system"])
	require.Equal(t, "test-topic-http", attrs["messaging.destination.name"])

	// Assert on propagation (parent-child relationships across async boundary)
	require.Equal(t, httpServerSpan.TraceID(), kafkaProducerSpan.TraceID(), "trace ID mismatch")
	require.Equal(
		t,
		httpServerSpan.SpanID(),
		kafkaProducerSpan.ParentSpanID(),
		"Kafka producer parent must be HTTP server",
	)
}
