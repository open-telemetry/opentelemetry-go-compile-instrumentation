// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/otelc/test/shared/grpcpb/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"go.opentelemetry.io/otelc/test/testutil"
)

func TestGRPCServerKafkaClient(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("kafka testcontainer not supported on windows")
	}
	testcontainers.SkipIfProviderIsNotHealthy(t)

	// Not using t.Parallel() here to prevent CI resource exhaustion 
	// from spinning up multiple heavy Kafka testcontainers simultaneously.
	testutil.Build(t, "", "grpcserverkafkaclient", "go", "build", "-a")

	brokers := startKafkaContainer(t)

	f := testutil.NewTestFixture(t)
	f.SetEnv("KAFKA_BROKERS", strings.Join(brokers, ","))

	frontPort := testutil.FreePort(t)
	f.Start("grpcserverkafkaclient", fmt.Sprintf("-front-port=%d", frontPort))
	testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", frontPort))

	// Send request to gRPC frontend
	target := fmt.Sprintf("127.0.0.1:%d", frontPort)
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()
	client := pb.NewGreeterClient(conn)

	resp, err := client.SayHello(t.Context(), &pb.HelloRequest{Name: "Kafka"})
	require.NoError(t, err)
	require.Equal(t, "frontend produced message to kafka", resp.Message)

	// Wait for the spans to be flushed
	f.WaitForSpans(2)

	// We expect exactly 1 trace with 2 spans:
	// 1. gRPC server (Frontend)
	// 2. Kafka producer (Frontend -> Broker)
	f.RequireTraceCount(1)
	f.RequireSpansPerTrace(2)

	grpcServerSpan := testutil.RequireSpan(
		t,
		f.Traces(),
		testutil.IsServer,
		func(s ptrace.Span) bool { return s.Name() == "greeter.Greeter/SayHello" },
	)
	kafkaProducerSpan := testutil.RequireSpan(
		t,
		f.Traces(),
		testutil.IsProducer,
		func(s ptrace.Span) bool { return s.Name() == "test-topic-grpc send" },
	)

	attrs := testutil.Attrs(kafkaProducerSpan)
	require.Equal(t, "kafka", attrs["messaging.system"])
	require.Equal(t, "test-topic-grpc", attrs["messaging.destination.name"])

	// Assert on propagation (parent-child relationships across async boundary)
	require.Equal(t, grpcServerSpan.TraceID(), kafkaProducerSpan.TraceID(), "trace ID mismatch")
	require.Equal(
		t,
		grpcServerSpan.SpanID(),
		kafkaProducerSpan.ParentSpanID(),
		"Kafka producer parent must be gRPC server",
	)
}
