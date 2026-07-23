// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main is a complex demo app combining gRPC server and SQL client for e2e testing.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"go.opentelemetry.io/otelc/test/shared/grpcpb/pb"
	"google.golang.org/grpc"

	_ "go.opentelemetry.io/otelc/test/shared/testdb"
)

var frontPort = flag.Int("front-port", 50051, "port for gRPC frontend")

type server struct {
	pb.UnimplementedGreeterServer
	db *sql.DB
}

func (s *server) SayHello(ctx context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, name FROM users WHERE name = ?", "alice")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return &pb.HelloReply{Message: "frontend querying database"}, nil
}

func main() {
	flag.Parse()

	db, err := sql.Open("testdb", "user:pass@tcp(127.0.0.1:3306)/testdb?charset=utf8")
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *frontPort))
	if err != nil {
		log.Fatalf("failed to listen on frontPort: %v", err)
	}
	grpcServer := grpc.NewServer()
	pb.RegisterGreeterServer(grpcServer, &server{db: db})

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
