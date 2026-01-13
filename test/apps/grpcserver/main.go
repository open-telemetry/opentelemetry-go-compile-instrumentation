// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a minimal gRPC server for integration testing.
// This server is designed to be instrumented with the otel compile-time tool.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"

	pb "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/apps/grpcserver/pb"
	"google.golang.org/grpc"
)

var port = flag.Int("port", 50051, "The server port")

type server struct {
	pb.UnimplementedGreeterServer
}

func (s *server) SayHello(ctx context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	slog.Info("received request", "name", in.GetName())
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
		slog.Info("received stream request", "name", in.GetName())
		if err := stream.Send(&pb.HelloReply{Message: "Hello " + in.GetName()}); err != nil {
			return err
		}
	}
}

func main() {
	flag.Parse()

	addr := fmt.Sprintf(":%d", *port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	defer lis.Close()

	s := grpc.NewServer()
	pb.RegisterGreeterServer(s, &server{})

	slog.Info("server started", "address", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
