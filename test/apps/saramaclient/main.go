// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a minimal IBM/sarama client for integration testing.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/IBM/sarama"
)

const topic = "orders"

// groupID is the consumer group name used by /consume-group. The integration
// test asserts it on the resulting consumer span.
const groupID = "orders-group"

var (
	brokerAddr      = flag.String("broker-addr", "localhost:9092", "Kafka broker address")
	groupBrokerAddr = flag.String("group-broker-addr", "", "Kafka broker for the consumer-group path (defaults to -broker-addr)")
	frontPort       = flag.Int("front-port", 8080, "port for HTTP frontend")
)

func main() {
	flag.Parse()

	// Clients are created per request: the mock brokers only exist for the
	// duration of one integration subtest, and a client created at startup
	// would try to refresh metadata against an address that is not live yet.
	mux := http.NewServeMux()
	mux.HandleFunc("/produce", produceHandler)
	mux.HandleFunc("/consume", consumeHandler)
	mux.HandleFunc("/consume-group", consumeGroupHandler)

	server := &http.Server{Addr: fmt.Sprintf(":%d", *frontPort), Handler: mux}
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("frontend server: %v", err)
		}
	}()
	log.Printf("listening on :%d", *frontPort)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}

func produceHandler(w http.ResponseWriter, r *http.Request) {
	producer, err := newProducer(*brokerAddr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer func() { _ = producer.Close() }()

	msg := &sarama.ProducerMessage{
		Topic: topic, Value: sarama.StringEncoder("hello"), Metadata: r.Context(),
	}
	partition, offset, err := producer.SendMessage(msg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Partition int32
		Offset    int64
		Headers   []sarama.RecordHeader
	}{partition, offset, msg.Headers})
}

func consumeHandler(w http.ResponseWriter, r *http.Request) {
	consumer, err := newConsumer(*brokerAddr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer func() { _ = consumer.Close() }()

	pc, err := consumer.ConsumePartition(topic, 0, sarama.OffsetOldest)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer func() {
		_ = pc.Close()
	}()

	select {
	case m := <-pc.Messages():
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "received topic=%s partition=%d offset=%d value=%s\n",
			m.Topic, m.Partition, m.Offset, string(m.Value))
	case err := <-pc.Errors():
		http.Error(w, err.Err.Error(), http.StatusBadGateway)
	case <-time.After(5 * time.Second):
		http.Error(w, "timed out waiting for message", http.StatusGatewayTimeout)
	}
}

// singleMessageHandler is a ConsumerGroupHandler that consumes one record and
// ends the session, so the HTTP request returns after a single round trip.
type singleMessageHandler struct {
	message *sarama.ConsumerMessage
	done    chan struct{}
}

func (h *singleMessageHandler) Setup(sarama.ConsumerGroupSession) error   { return nil }
func (h *singleMessageHandler) Cleanup(sarama.ConsumerGroupSession) error { return nil }

func (h *singleMessageHandler) ConsumeClaim(sess sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for {
		select {
		case msg, ok := <-claim.Messages():
			if !ok {
				return nil
			}
			sess.MarkMessage(msg, "")
			h.message = msg
			close(h.done)
			// Nothing else to consume; ending the session returns from
			// Consume after Cleanup.
			return nil
		case <-sess.Context().Done():
			return nil
		}
	}
}

func consumeGroupHandler(w http.ResponseWriter, r *http.Request) {
	addr := *groupBrokerAddr
	if addr == "" {
		addr = *brokerAddr
	}

	group, err := newConsumerGroup(addr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer func() { _ = group.Close() }()

	h := &singleMessageHandler{done: make(chan struct{})}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	consumeErr := make(chan error, 1)
	go func() {
		consumeErr <- group.Consume(ctx, []string{topic}, h)
	}()

	select {
	case <-h.done:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Group     string `json:"group"`
			Topic     string `json:"topic"`
			Partition int32  `json:"partition"`
			Offset    int64  `json:"offset"`
			Value     string `json:"value"`
		}{groupID, h.message.Topic, h.message.Partition, h.message.Offset, string(h.message.Value)})
	case err := <-consumeErr:
		http.Error(w, fmt.Sprintf("consumer group failed: %v", err), http.StatusBadGateway)
	case <-ctx.Done():
		http.Error(w, "timed out waiting for group message", http.StatusGatewayTimeout)
	}
}

// newProducer creates a synchronous producer. Version is pinned to 0.11 (record
// headers) so the trace context injected by the instrumentation actually rides
// on the wire, and the mock broker's record-batch responses match the request
// versions.
func newProducer(addr string) (sarama.SyncProducer, error) {
	config := sarama.NewConfig()
	config.Version = sarama.V0_11_0_0
	config.Producer.Return.Successes = true
	config.Producer.Return.Errors = true
	return sarama.NewSyncProducer([]string{addr}, config)
}

// newConsumer creates a plain consumer. See newProducer for the version choice.
func newConsumer(addr string) (sarama.Consumer, error) {
	config := sarama.NewConfig()
	config.Version = sarama.V0_11_0_0
	config.Consumer.Return.Errors = true
	return sarama.NewConsumer([]string{addr}, config)
}

// newConsumerGroup creates a consumer group. A 2.0 protocol version is used so
// the mock coordinator's Join/Sync/Heartbeat/OffsetCommit round trips use the
// request versions the mock broker emits.
func newConsumerGroup(addr string) (sarama.ConsumerGroup, error) {
	config := sarama.NewConfig()
	config.Version = sarama.V2_0_0_0
	config.Consumer.Return.Errors = true
	config.Consumer.Offsets.AutoCommit.Enable = false
	config.Consumer.Group.Session.Timeout = time.Second
	config.Consumer.Group.Heartbeat.Interval = 50 * time.Millisecond
	config.Consumer.Group.Rebalance.Retry.Backoff = 0
	return sarama.NewConsumerGroup([]string{addr}, groupID, config)
}
