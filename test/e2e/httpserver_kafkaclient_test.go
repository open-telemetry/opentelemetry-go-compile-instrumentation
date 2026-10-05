//go:build e2e

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"

	"go.opentelemetry.io/otelc/test/testutil"
)

func TestHTTPServerKafkaClient(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("kafka testcontainer not supported on windows")
	}
	testcontainers.SkipIfProviderIsNotHealthy(t)

	brokers := startKafkaContainer(t)

	f := testutil.NewTestFixture(t)
	f.SetEnv("KAFKA_BROKERS", strings.Join(brokers, ","))

	frontPort := testutil.FreePort(t)
	addr := fmt.Sprintf("http://127.0.0.1:%d", frontPort)

	f.BuildAndStart("httpserverkafkaclient", fmt.Sprintf("-front-port=%d", frontPort))
	testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", frontPort))

	f.BuildAndRun("httpclient", "-addr", addr, "-name", "test")

	// BuildAndRun returns once the client exits, but the server span ends after
	// the response is written and the producer span ends after the broker ack,
	// so both may still be in flight. Wait for all three.
	f.WaitForSpans(3)

	// One distributed trace with three spans:
	// HTTP client -> HTTP server -> Kafka producer
	f.RequireTraceCount(1)
	f.RequireSpansPerTrace(3)

	httpClientSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsClient,
		testutil.HasAttributeContaining(string(semconv.URLFullKey), "/hello"),
	)
	httpServerSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsServer,
		testutil.HasAttribute(string(semconv.URLPathKey), "/hello"),
		testutil.HasAttribute(string(semconv.HTTPResponseStatusCodeKey), int64(200)),
	)
	kafkaProducerSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsProducer,
		testutil.HasName("test-topic-http send"),
	)

	require.Equal(t, httpClientSpan.TraceID(), httpServerSpan.TraceID(), "HTTP client and server must share a trace ID")
	require.Equal(
		t,
		httpServerSpan.TraceID(),
		kafkaProducerSpan.TraceID(),
		"HTTP server and Kafka producer must share a trace ID",
	)
	require.Equal(t, httpClientSpan.SpanID(), httpServerSpan.ParentSpanID(), "HTTP server parent must be HTTP client")
	require.Equal(
		t,
		httpServerSpan.SpanID(),
		kafkaProducerSpan.ParentSpanID(),
		"Kafka producer parent must be HTTP server",
	)
}
