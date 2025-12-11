// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	pb "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/demo/grpc/server/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	addr     = flag.String("addr", "localhost:50051", "the address to connect to")
	name     = flag.String("name", "world", "Name to greet")
	stream   = flag.Bool("stream", false, "Use streaming RPC")
	shutdown = flag.Bool("shutdown", false, "Shutdown the server")
	count    = flag.Int("count", 1, "Number of requests to send")
	logLevel = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	logger   *slog.Logger
)

func main() {
	flag.Parse()

	// Initialize logger with appropriate level
	var level slog.Level
	switch *logLevel {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: level,
	}
	logger = slog.New(slog.NewJSONHandler(os.Stdout, opts))

	logger.Info("client starting",
		"server_address", *addr,
		"stream", *stream,
		"shutdown", *shutdown,
		"count", *count,
		"log_level", *logLevel)

	// Set up a connection to the server
	conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		logger.Error("did not connect", "error", err)
		os.Exit(1)
	}
	defer conn.Close()
	c := pb.NewGreeterClient(conn)

	// Contact the server and print out its response
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	defer finish()

	if *shutdown {
		// Send shutdown request
		r, err := c.Shutdown(ctx, &pb.ShutdownRequest{})
		if err != nil {
			logger.Error("could not send shutdown", "error", err)
			os.Exit(1)
		}
		logger.Info("shutdown response", "message", r.GetMessage())
		return
	}

	if *stream {
		runStream(ctx, c)
		return
	}

	runUnary(ctx, c)
}

func runUnary(ctx context.Context, c pb.GreeterClient) {
	// Unary RPC - loop count times with delay
	successCount := 0
	failureCount := 0

	for i := 0; i < *count; i++ {
		requestName := *name
		if *count > 1 {
			requestName = fmt.Sprintf("%s-%d", *name, i+1)
		}

		logger.Info("sending request",
			"request_number", i+1,
			"total_requests", *count,
			"name", requestName)

		r, err := c.SayHello(ctx, &pb.HelloRequest{Name: requestName})
		if err != nil {
			logger.Error("could not greet",
				"request_number", i+1,
				"error", err)
			failureCount++
			continue
		}
		logger.Info("greeting",
			"request_number", i+1,
			"message", r.GetMessage())
		successCount++

		// Add delay between requests when sending multiple
		if i < *count-1 {
			time.Sleep(500 * time.Millisecond)
		}
	}

	logger.Info("unary RPC completed",
		"total_requests", *count,
		"successful", successCount,
		"failed", failureCount)
}

func runStream(ctx context.Context, c pb.GreeterClient) {
	// Streaming RPC
	streamClient, err := c.SayHelloStream(ctx)
	if err != nil {
		logger.Error("could not create stream", "error", err)
		os.Exit(1)
	}

	// Start receiving responses in a goroutine
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			reply, err := streamClient.Recv()
			if err == io.EOF {
				return
			}
			if err != nil {
				logger.Error("could not receive", "error", err)
				return
			}
			logger.Info("stream response", "message", reply.GetMessage())
		}
	}()

	// Send multiple requests
	for i := 0; i < *count; i++ {
		req := &pb.HelloRequest{Name: *name + " " + time.Now().Format("15:04:05")}
		if err := streamClient.Send(req); err != nil {
			logger.Error("could not send", "error", err)
			break
		}
		logger.Info("sent request", "name", req.GetName())
		time.Sleep(500 * time.Millisecond)
	}

	// Close sending side
	if err := streamClient.CloseSend(); err != nil {
		logger.Error("could not close send", "error", err)
	}

	// Wait for receiver to finish
	<-done
}

func finish() {
	logger.Info("client finished")
}
