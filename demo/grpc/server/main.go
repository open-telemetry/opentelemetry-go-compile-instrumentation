// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:generate mkdir -p pb
//go:generate protoc --go_out=pb --go_opt=paths=source_relative --go-grpc_out=pb --go-grpc_opt=paths=source_relative greeter.proto

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"time"

	pb "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/demo/grpc/server/pb"
	"google.golang.org/grpc"
)

var (
	port     = flag.Int("port", 50051, "The server port")
	logLevel = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	logger   *slog.Logger
)

type server struct {
	pb.UnimplementedGreeterServer
}

func (s *server) SayHello(ctx context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	logger.Info("received request", "name", in.GetName())
	return &pb.HelloReply{Message: "Hello " + in.GetName()}, nil
}

func (s *server) SayHelloStream(stream pb.Greeter_SayHelloStreamServer) error {
	for {
		in, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		logger.Info("received stream request", "name", in.GetName())
		if err := stream.Send(&pb.HelloReply{Message: "Hello " + in.GetName()}); err != nil {
			return err
		}
	}
}

func (s *server) Shutdown(ctx context.Context, in *pb.ShutdownRequest) (*pb.ShutdownReply, error) {
	logger.Info("shutdown request received")
	go func() {
		time.Sleep(100 * time.Millisecond) // Give time to send response
		os.Exit(0)
	}()
	return &pb.ShutdownReply{Message: "Server is shutting down"}, nil
}

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

	addr := fmt.Sprintf(":%d", *port)
	logger.Info("server starting", "address", addr, "log_level", *logLevel)

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		logger.Error("failed to listen", "error", err)
		os.Exit(1)
	}
	defer lis.Close()

	runServer(lis)
}

func runServer(lis net.Listener) {
	s := grpc.NewServer()
	pb.RegisterGreeterServer(s, &server{})
	logger.Info("server listening", "address", lis.Addr())
	logger.Info("server started")
	if err := s.Serve(lis); err != nil {
		logger.Error("failed to serve", "error", err)
		os.Exit(1)
	}
}
