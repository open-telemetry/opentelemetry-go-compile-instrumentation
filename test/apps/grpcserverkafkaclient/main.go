// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main is a complex demo app combining gRPC server and Kafka producer for e2e testing.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	kafka "github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otelc/test/shared/grpcpb/pb"
	"google.golang.org/grpc"
)

var frontPort = flag.Int("front-port", 50051, "port for gRPC frontend")

func brokers() []string {
	if v := os.Getenv("KAFKA_BROKERS"); v != "" {
		return strings.Split(v, ",")
	}
	return []string{"localhost:9092"}
}

// ensureTopic creates the topic up front (best-effort) so a single WriteMessages
// call succeeds and emits exactly one producer span. CreateTopics is not instrumented.
func ensureTopic(ctx context.Context, topic string) {
	client := &kafka.Client{Addr: kafka.TCP(brokers()...)}
	_, _ = client.CreateTopics(ctx, &kafka.CreateTopicsRequest{
		Topics: []kafka.TopicConfig{{
			Topic:             topic,
			NumPartitions:     1,
			ReplicationFactor: 1,
		}},
	})
}

type server struct {
	pb.UnimplementedGreeterServer
}

func (s *server) SayHello(ctx context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	topic := "test-topic-grpc"
	ensureTopic(ctx, topic)

	writer := &kafka.Writer{
		Addr:         kafka.TCP(brokers()...),
		Topic:        topic,
		Balancer:     &kafka.LeastBytes{},
		BatchSize:    1,
		BatchTimeout: 10 * time.Millisecond,
		RequiredAcks: kafka.RequireAll,
	}
	defer writer.Close()

	err := writer.WriteMessages(ctx, kafka.Message{
		Key:   []byte("test-key"),
		Value: []byte("test-message"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to write to kafka: %v", err)
	}

	return &pb.HelloReply{Message: "frontend produced message to kafka"}, nil
}

func main() {
	flag.Parse()

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *frontPort))
	if err != nil {
		log.Fatalf("failed to listen on frontPort: %v", err)
	}
	grpcServer := grpc.NewServer()
	pb.RegisterGreeterServer(grpcServer, &server{})

	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("frontend server failed: %v", err)
		}
	}()

	// Wait for shutdown signal
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	grpcServer.GracefulStop()
}
