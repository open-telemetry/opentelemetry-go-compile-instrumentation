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

var (
	brokerAddr = flag.String("broker-addr", "localhost:9092", "Kafka broker address")
	frontPort  = flag.Int("front-port", 8080, "port for HTTP frontend")
)

func main() {
	flag.Parse()

	producer, err := newProducer()
	if err != nil {
		log.Fatalf("failed to create producer: %v", err)
	}
	defer func() {
		_ = producer.Close()
	}()

	consumer, err := newConsumer()
	if err != nil {
		log.Fatalf("failed to create consumer: %v", err)
	}
	defer func() {
		_ = consumer.Close()
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/produce", func(w http.ResponseWriter, r *http.Request) {
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
	})
	mux.HandleFunc("/consume", func(w http.ResponseWriter, r *http.Request) {
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
	})

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

// newProducer creates a synchronous producer. Version is pinned to 0.11 (record
// headers) so the trace context injected by the instrumentation actually rides
// on the wire, and the mock broker's record-batch responses match the request
// versions.
func newProducer() (sarama.SyncProducer, error) {
	config := sarama.NewConfig()
	config.Version = sarama.V0_11_0_0
	config.Producer.Return.Successes = true
	config.Producer.Return.Errors = true
	return sarama.NewSyncProducer([]string{*brokerAddr}, config)
}

// newConsumer creates a plain consumer. See newProducer for the version choice.
func newConsumer() (sarama.Consumer, error) {
	config := sarama.NewConfig()
	config.Version = sarama.V0_11_0_0
	config.Consumer.Return.Errors = true
	return sarama.NewConsumer([]string{*brokerAddr}, config)
}
