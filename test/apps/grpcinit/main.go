// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main calls a gRPC server through a client created during package
// initialization.
package main

import (
	"context"
	"flag"
	"log"
	"log/slog"
	"time"

	"example.com/grpcinit/early"

	"go.opentelemetry.io/otelc/test/shared/grpcpb/pb"
)

func main() {
	flag.String("addr", "", "The server address, read by package early")
	flag.Parse()
	defer early.Conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := pb.NewGreeterClient(early.Conn).SayHello(ctx, &pb.HelloRequest{Name: "InitTest"})
	if err != nil {
		log.Fatalf("failed to call SayHello: %v", err)
	}
	slog.Info("greeting", "message", resp.GetMessage())
}
