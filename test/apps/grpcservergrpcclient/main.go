// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main is a complex demo app combining gRPC client and server for e2e testing.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"go.opentelemetry.io/otelc/test/shared/grpcpb/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	frontPort = flag.Int("front-port", 8080, "port for gRPC frontend")
	backPort  = flag.Int("back-port", 50051, "port for gRPC backend")
)

type backendServer struct {
	pb.UnimplementedGreeterServer
}

func (s *backendServer) SayHello(ctx context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	return &pb.HelloReply{Message: "Hello " + in.GetName()}, nil
}

type frontendServer struct {
	pb.UnimplementedGreeterServer
	backendClient pb.GreeterClient
}

func (s *frontendServer) SayHello(ctx context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	resp, err := s.backendClient.SayHello(ctx, in)
	if err != nil {
		return nil, err
	}
	return &pb.HelloReply{Message: "frontend calling backend, response: " + resp.GetMessage()}, nil
}

func main() {
	flag.Parse()

	// Start backend
	backLis, err := net.Listen("tcp", fmt.Sprintf(":%d", *backPort))
	if err != nil {
		log.Fatalf("failed to listen on backPort: %v", err)
	}
	backGrpcServer := grpc.NewServer()
	pb.RegisterGreeterServer(backGrpcServer, &backendServer{})

	go func() {
		if err := backGrpcServer.Serve(backLis); err != nil {
			log.Fatalf("backend server failed: %v", err)
		}
	}()

	// Connect to backend
	conn, err := grpc.NewClient(fmt.Sprintf("127.0.0.1:%d", *backPort), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("failed to connect to backend: %v", err)
	}
	defer conn.Close()
	client := pb.NewGreeterClient(conn)

	// Start frontend
	frontLis, err := net.Listen("tcp", fmt.Sprintf(":%d", *frontPort))
	if err != nil {
		log.Fatalf("failed to listen on frontPort: %v", err)
	}
	frontGrpcServer := grpc.NewServer()
	pb.RegisterGreeterServer(frontGrpcServer, &frontendServer{backendClient: client})

	go func() {
		if err := frontGrpcServer.Serve(frontLis); err != nil {
			log.Fatalf("frontend server failed: %v", err)
		}
	}()

	// Wait for shutdown signal
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	backGrpcServer.GracefulStop()
	frontGrpcServer.GracefulStop()
}
