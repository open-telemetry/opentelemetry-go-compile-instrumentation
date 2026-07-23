// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main is a complex demo app combining net/http and gRPC for e2e testing.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"

	"go.opentelemetry.io/otelc/test/shared/grpcpb/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	frontPort = flag.String("front-port", "8080", "port for net/http frontend")
	backPort  = flag.Int("back-port", 50051, "port for gRPC backend")
)

type server struct {
	pb.UnimplementedGreeterServer
}

func (s *server) SayHello(ctx context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	return &pb.HelloReply{Message: "Hello " + in.GetName()}, nil
}

func main() {
	flag.Parse()

	// Start backend (gRPC)
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *backPort))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterGreeterServer(grpcServer, &server{})

	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()

	// Start frontend (net/http)
	mux := http.NewServeMux()
	mux.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
		// Create a request to the backend, explicitly passing the context for propagation
		conn, err := grpc.NewClient(fmt.Sprintf("127.0.0.1:%d", *backPort), grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer conn.Close()

		client := pb.NewGreeterClient(conn)
		resp, err := client.SayHello(r.Context(), &pb.HelloRequest{Name: "otelc"})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

		fmt.Fprintf(w, "frontend calling backend, response: %s", resp.GetMessage())
	})

	frontendServer := &http.Server{
		Addr:    fmt.Sprintf(":%s", *frontPort),
		Handler: mux,
	}

	go func() {
		if err := frontendServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("frontend error: %v", err)
		}
	}()

	// Keep alive
	<-context.Background().Done()
}
