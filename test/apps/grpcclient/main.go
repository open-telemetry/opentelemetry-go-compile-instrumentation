// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a minimal gRPC client for integration testing.
// This client is designed to be instrumented with the otelc compile-time tool.
package main

import (
	"context"
	"flag"
	"io"
	"log"
	"log/slog"
	"time"

	pb "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/apps/grpcserver/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	addr    = flag.String("addr", "localhost:50051", "The server address")
	name    = flag.String("name", "world", "The name to greet")
	stream  = flag.Bool("stream", false, "Use streaming RPC")
	count   = flag.Int("count", 1, "Number of requests to make (for streaming)")
	dialAPI = flag.String("dial-api", "newclient", "gRPC client API to use: newclient, dialcontext, or dial")
)

func main() {
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := dial(ctx)
	defer conn.Close()

	client := pb.NewGreeterClient(conn)

	if *stream {
		doStreaming(ctx, client)
	} else {
		doUnary(ctx, client)
	}
}

func dial(ctx context.Context) *grpc.ClientConn {
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}

	var (
		conn *grpc.ClientConn
		err  error
	)
	switch *dialAPI {
	case "newclient":
		conn, err = grpc.NewClient(*addr, opts...)
	case "dialcontext":
		conn, err = grpc.DialContext(ctx, *addr, opts...)
	case "dial":
		conn, err = grpc.Dial(*addr, opts...) //nolint:staticcheck // Exercise legacy Dial join-point coverage.
	default:
		log.Fatalf("unsupported -dial-api %q", *dialAPI)
	}
	if err != nil {
		log.Fatalf("failed to connect with %s: %v", *dialAPI, err)
	}
	return conn
}

func doUnary(ctx context.Context, client pb.GreeterClient) {
	resp, err := client.SayHello(ctx, &pb.HelloRequest{Name: *name})
	if err != nil {
		log.Fatalf("failed to call SayHello: %v", err)
	}
	slog.Info("greeting", "message", resp.GetMessage())
}

func doStreaming(ctx context.Context, client pb.GreeterClient) {
	stream, err := client.SayHelloStream(ctx)
	if err != nil {
		log.Fatalf("failed to call SayHelloStream: %v", err)
	}

	// Send requests
	for i := 0; i < *count; i++ {
		if err := stream.Send(&pb.HelloRequest{Name: *name}); err != nil {
			log.Fatalf("failed to send: %v", err)
		}
	}
	if err := stream.CloseSend(); err != nil {
		log.Fatalf("failed to close send: %v", err)
	}

	// Receive responses
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("failed to receive: %v", err)
		}
		slog.Info("stream response", "message", resp.GetMessage())
	}
}
