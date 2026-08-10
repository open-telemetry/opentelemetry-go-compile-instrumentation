// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a minimal gRPC client for integration testing.
// This client is designed to be instrumented with the otelc compile-time tool.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"go.opentelemetry.io/otelc/test/shared/grpcpb/pb"
)

var (
	addr   = flag.String("addr", "localhost:50051", "The server address")
	name   = flag.String("name", "world", "The name to greet")
	stream = flag.Bool("stream", false, "Use streaming RPC")
	count  = flag.Int("count", 1, "Number of requests to make (for streaming)")
)

func run() error {
	flag.Parse()

	conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer conn.Close()

	client := pb.NewGreeterClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if *stream {
		doStreaming(ctx, client)
	} else {
		doUnary(ctx, client)
	}
}

func doUnary(ctx context.Context, client pb.GreeterClient) {
	resp, err := client.SayHello(ctx, &pb.HelloRequest{Name: *name})
	if err != nil {
		return fmt.Errorf("failed to call SayHello: %w", err)
	}
	slog.Info("greeting", "message", resp.GetMessage())
}

func doStreaming(ctx context.Context, client pb.GreeterClient) {
	stream, err := client.SayHelloStream(ctx)
	if err != nil {
		return fmt.Errorf("failed to call SayHelloStream: %w", err)
	}

	// Send requests
	for i := 0; i < *count; i++ {
		if err := stream.Send(&pb.HelloRequest{Name: *name}); err != nil {
			return fmt.Errorf("failed to send: %w", err)
		}
	}
	if err := stream.CloseSend(); err != nil {
		return fmt.Errorf("failed to close send: %w", err)
	}

	// Receive responses
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to receive: %w", err)
		}
		slog.Info("stream response", "message", resp.GetMessage())
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Printf("error: %v\n", err)
		os.Exit(1)
	}
}
