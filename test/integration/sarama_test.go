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

	// Consumer-group round trip: NewConsumerGroup forms a session with the
	// coordinator and consumes one record. Its consumer span must carry the
	// group id that BeforeNewConsumerGroup tags onto the private group config,
	// proving the config-copy path works end to end (nil/overwrite paths are
	// covered by unit tests).
	groupBroker := startKafkaGroupMock(t)
	groupPort := testutil.FreePort(t)
	gf := testutil.NewTestFixture(t)
	gf.Start("saramaclient",
		fmt.Sprintf("-broker-addr=%s", broker.Addr()),
		fmt.Sprintf("-group-broker-addr=%s", groupBroker.Addr()),
		fmt.Sprintf("-front-port=%d", groupPort),
	)
	testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", groupPort))

	groupBody := hitEndpoint(t, groupPort, "/consume-group")
	var consumed struct {
		Group     string
		Topic     string
		Partition int32
		Offset    int64
		Value     string
	}
	require.NoError(t, json.Unmarshal([]byte(groupBody), &consumed))
	require.Equal(t, "orders-group", consumed.Group)
	require.Equal(t, "orders", consumed.Topic)
	require.Equal(t, "hello", consumed.Value)

	gf.WaitForSpans(2)
	testutil.RequireSpan(
		t,
		gf.Traces(),
		testutil.IsServer,
		func(s ptrace.Span) bool { return s.Name() == "GET /consume-group" },
	)
	groupSpan := testutil.RequireSpan(
		t,
		gf.Traces(),
		testutil.IsConsumer,
		func(s ptrace.Span) bool { return s.Name() == "orders receive" },
	)
	// The ConsumerGroup interceptor runs on sarama's broker-fetch goroutine,
	// outside the HTTP request context, so this consumer span is not a child of
	// the server span (unlike ConsumePartition, whose interceptor the request
	// goroutine invokes). What this round trip must prove is that the span
	// carries the group id BeforeNewConsumerGroup tagged onto its private
	// config copy, alongside the receive semconv.
	require.NotEmpty(t, groupSpan.TraceID())
	testutil.RequireSaramaConsumerSemconv(t, groupSpan, "orders", "0", 0)
	testutil.RequireAttribute(t, groupSpan, "messaging.consumer.group.name", "orders-group")
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

// startKafkaGroupMock starts an in-process Kafka broker that coordinates one
// consumer-group session ("orders-group" over "orders" partition 0), delivers
// one record and accepts its leave/commit.
func startKafkaGroupMock(t *testing.T) *sarama.MockBroker {
	t.Helper()
	broker := sarama.NewMockBroker(t, 2)
	t.Cleanup(broker.Close)

	fetch := &sarama.FetchResponse{Version: 4}
	fetch.AddRecord("orders", 0, nil, sarama.StringEncoder("hello"), 0)
	fetch.GetBlock("orders", 0).HighWaterMarkOffset = 1
	emptyFetch := &sarama.FetchResponse{Version: 4}
	emptyFetch.AddError("orders", 0, sarama.ErrNoError)
	emptyFetch.GetBlock("orders", 0).HighWaterMarkOffset = 1

	broker.SetHandlerByMap(map[string]sarama.MockResponse{
		"ApiVersionsRequest": sarama.NewMockApiVersionsResponse(t).SetApiKeys([]sarama.ApiVersionsResponseKey{
			{ApiKey: 0, MinVersion: 0, MaxVersion: 3},  // Produce
			{ApiKey: 1, MinVersion: 0, MaxVersion: 4},  // Fetch
			{ApiKey: 2, MinVersion: 0, MaxVersion: 5},  // Offset
			{ApiKey: 3, MinVersion: 0, MaxVersion: 8},  // Metadata
			{ApiKey: 8, MinVersion: 0, MaxVersion: 3},  // OffsetCommit
			{ApiKey: 9, MinVersion: 0, MaxVersion: 5},  // OffsetFetch
			{ApiKey: 10, MinVersion: 0, MaxVersion: 4}, // FindCoordinator
			{ApiKey: 11, MinVersion: 0, MaxVersion: 4}, // JoinGroup
			{ApiKey: 12, MinVersion: 0, MaxVersion: 3}, // Heartbeat
			{ApiKey: 13, MinVersion: 0, MaxVersion: 3}, // LeaveGroup
			{ApiKey: 14, MinVersion: 0, MaxVersion: 3}, // SyncGroup
		}),
		"MetadataRequest": sarama.NewMockMetadataResponse(t).
			SetBroker(broker.Addr(), broker.BrokerID()).
			SetLeader("orders", 0, broker.BrokerID()),
		"OffsetRequest": sarama.NewMockOffsetResponse(t).
			SetOffset("orders", 0, sarama.OffsetOldest, 0).
			SetOffset("orders", 0, sarama.OffsetNewest, 1),
		"FindCoordinatorRequest": sarama.NewMockFindCoordinatorResponse(t).
			SetCoordinator(sarama.CoordinatorGroup, "orders-group", broker),
		"JoinGroupRequest": sarama.NewMockJoinGroupResponse(t).
			SetGroupProtocol(sarama.RangeBalanceStrategyName).
			SetMemberId("test-member"),
		"SyncGroupRequest": sarama.NewMockSyncGroupResponse(t).SetMemberAssignment(
			&sarama.ConsumerGroupMemberAssignment{
				Version: 0,
				Topics:  map[string][]int32{"orders": {0}},
			}),
		"HeartbeatRequest":    sarama.NewMockHeartbeatResponse(t),
		"LeaveGroupRequest":   sarama.NewMockLeaveGroupResponse(t),
		"OffsetCommitRequest": sarama.NewMockOffsetCommitResponse(t),
		"OffsetFetchRequest": sarama.NewMockOffsetFetchResponse(t).
			SetOffset("orders-group", "orders", 0, 0, "", sarama.ErrNoError).
			SetError(sarama.ErrNoError),
		// One message, then an empty fetch so the handler ends the session.
		"FetchRequest": sarama.NewMockSequence(fetch, emptyFetch),
	})
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
