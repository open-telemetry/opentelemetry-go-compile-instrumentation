// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/IBM/sarama"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"go.opentelemetry.io/otelc/test/testutil"
)

func TestSaramaClient(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "saramaclient", "go", "build", "-a")

	broker := startKafkaMock(t)

	f := testutil.NewTestFixture(t)
	frontPort := testutil.FreePort(t)
	f.Start("saramaclient",
		fmt.Sprintf("-broker-addr=%s", broker.Addr()),
		fmt.Sprintf("-front-port=%d", frontPort),
	)
	testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", frontPort))

	// Produce: one SyncProducer.SendMessage against the mock broker produces
	// one producer span.
	produceBody := hitEndpoint(t, frontPort, "/produce")
	var produced struct {
		Partition int32
		Offset    int64
		Headers   []sarama.RecordHeader
	}
	require.NoError(t, json.Unmarshal([]byte(produceBody), &produced))
	require.Equal(t, int32(0), produced.Partition)

	f.WaitForSpans(2)
	httpSpan := testutil.RequireSpan(
		t,
		f.Traces(),
		testutil.IsServer,
		func(s ptrace.Span) bool { return s.Name() == "GET /produce" },
	)
	producerSpan := testutil.RequireSpan(
		t,
		f.Traces(),
		testutil.IsProducer,
		func(s ptrace.Span) bool { return s.Name() == "orders send" },
	)
	require.Equal(t, httpSpan.TraceID(), producerSpan.TraceID())
	require.Equal(t, httpSpan.SpanID(), producerSpan.ParentSpanID())
	testutil.RequireSaramaProducerSemconv(t, producerSpan, "orders", broker.Addr())

	var traceparent string
	var headers []*sarama.RecordHeader
	for i := range produced.Headers {
		header := &produced.Headers[i]
		headers = append(headers, header)
		if string(header.Key) == "traceparent" {
			traceparent = string(header.Value)
		}
	}
	require.Equal(t, fmt.Sprintf("00-%s-%s-01", producerSpan.TraceID(), producerSpan.SpanID()), traceparent)

	// MockBroker does not persist produced records. Feed the actual headers
	// returned by SendMessage into a record-batch response; do not synthesize
	// trace context from the recorded spans. This verifies producer injection
	// and consumer extraction, but not broker persistence of the produce frame.
	fetch := &sarama.FetchResponse{Version: 4}
	fetch.AddRecord("orders", 0, nil, sarama.StringEncoder("hello"), 0)
	block := fetch.GetBlock("orders", 0)
	block.HighWaterMarkOffset = 1
	block.RecordsSet[0].RecordBatch.Records[0].Headers = headers
	emptyFetch := &sarama.FetchResponse{Version: 4}
	emptyFetch.AddError("orders", 0, sarama.ErrNoError)
	emptyFetch.GetBlock("orders", 0).HighWaterMarkOffset = 1
	handlers := kafkaMockHandlers(t, broker)
	handlers["FetchRequest"] = sarama.NewMockSequence(fetch, emptyFetch)
	broker.SetHandlerByMap(handlers)
	consumeBody := hitEndpoint(t, frontPort, "/consume")
	require.Contains(t, consumeBody, "received")

	f.WaitForSpans(4)
	consumerSpan := testutil.RequireSpan(
		t,
		f.Traces(),
		testutil.IsConsumer,
		func(s ptrace.Span) bool { return s.Name() == "orders receive" },
	)
	require.Equal(t, producerSpan.TraceID(), consumerSpan.TraceID())
	require.Equal(t, producerSpan.SpanID(), consumerSpan.ParentSpanID())
	testutil.RequireSaramaConsumerSemconv(t, consumerSpan, "orders", "0", 0)
}

// startKafkaMock starts an in-process Kafka broker that accepts a metadata
// round-trip, a successful produce, an offset lookup and a single fetch.
func startKafkaMock(t *testing.T) *sarama.MockBroker {
	t.Helper()
	broker := sarama.NewMockBroker(t, 1)
	broker.SetHandlerByMap(kafkaMockHandlers(t, broker))
	t.Cleanup(broker.Close)
	return broker
}

func kafkaMockHandlers(t *testing.T, broker *sarama.MockBroker) map[string]sarama.MockResponse {
	t.Helper()
	return map[string]sarama.MockResponse{
		// Advertise all APIs used by the test client, not only Produce/Fetch.
		"ApiVersionsRequest": sarama.NewMockApiVersionsResponse(t).SetApiKeys([]sarama.ApiVersionsResponseKey{
			{ApiKey: 0, MinVersion: 0, MaxVersion: 3}, // Produce
			{ApiKey: 1, MinVersion: 0, MaxVersion: 4}, // Fetch
			{ApiKey: 2, MinVersion: 0, MaxVersion: 1}, // Offset
			{ApiKey: 3, MinVersion: 0, MaxVersion: 1}, // Metadata
		}),
		"MetadataRequest": sarama.NewMockMetadataResponse(t).
			SetBroker(broker.Addr(), broker.BrokerID()).
			SetLeader("orders", 0, broker.BrokerID()),
		"ProduceRequest": sarama.NewMockProduceResponse(t),
		"OffsetRequest": sarama.NewMockOffsetResponse(t).
			SetOffset("orders", 0, sarama.OffsetNewest, 1).
			SetOffset("orders", 0, sarama.OffsetOldest, 0),
	}
}
