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

func TestGRPCServerKafkaClient(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("kafka testcontainer not supported on windows")
	}
	testcontainers.SkipIfProviderIsNotHealthy(t)

	brokers := startKafkaContainer(t)

	f := testutil.NewTestFixture(t)
	f.SetEnv("KAFKA_BROKERS", strings.Join(brokers, ","))

	frontPort := testutil.FreePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", frontPort)

	f.BuildAndStart("grpcserverkafkaclient", fmt.Sprintf("-front-port=%d", frontPort))
	testutil.WaitForTCP(t, addr)

	f.BuildAndRun("grpcclient", "-addr", addr, "-name", "test")

	// BuildAndRun returns once the client exits, but the server span ends after
	// the response is written and the producer span ends after the broker ack,
	// so both may still be in flight. Wait for all three.
	f.WaitForSpans(3)

	// One distributed trace with three spans:
	// gRPC client -> gRPC server -> Kafka producer
	f.RequireTraceCount(1)
	f.RequireSpansPerTrace(3)

	grpcClientSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsClient,
		testutil.HasAttribute(string(semconv.RPCSystemKey), "grpc"),
	)
	grpcServerSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsServer,
		testutil.HasAttribute(string(semconv.RPCSystemKey), "grpc"),
		testutil.HasAttribute(string(semconv.RPCGRPCStatusCodeKey), int64(0)),
	)
	kafkaProducerSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsProducer,
		testutil.HasName("test-topic-grpc send"),
	)

	require.Equal(t, grpcClientSpan.TraceID(), grpcServerSpan.TraceID(), "gRPC client and server must share a trace ID")
	require.Equal(
		t,
		grpcServerSpan.TraceID(),
		kafkaProducerSpan.TraceID(),
		"gRPC server and Kafka producer must share a trace ID",
	)
	require.Equal(t, grpcClientSpan.SpanID(), grpcServerSpan.ParentSpanID(), "gRPC server parent must be gRPC client")
	require.Equal(
		t,
		grpcServerSpan.SpanID(),
		kafkaProducerSpan.ParentSpanID(),
		"Kafka producer parent must be gRPC server",
	)
	require.True(t, grpcClientSpan.ParentSpanID().IsEmpty(), "gRPC client span must be the trace root")
}
