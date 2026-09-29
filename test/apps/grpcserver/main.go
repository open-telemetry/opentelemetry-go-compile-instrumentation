// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a minimal gRPC server for integration testing.
// This server is designed to be instrumented with the otelc compile-time tool.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"

	"go.opentelemetry.io/otelc/test/shared/grpcpb/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/stats"
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

// rpcMethodKey carries an RPC's method name from TagRPC to HandleRPC.
type rpcMethodKey struct{}

// rpcTracker logs a line once an RPC has fully completed, giving tests a point
// they can synchronise on.
//
// gRPC calls stats handlers in the order their ServerOptions were supplied, and
// the instrumentation injects its own handler ahead of the application's. So by
// the time this handler sees stats.End, the instrumentation has already ended
// the server span and handed it to the span processor. That ordering is what
// makes the log line useful: a test cannot instead treat the client holding its
// response as proof the span is buffered, because gRPC fires stats.End only
// after the response has been written.
type rpcTracker struct{}

func (rpcTracker) TagRPC(ctx context.Context, info *stats.RPCTagInfo) context.Context {
	return context.WithValue(ctx, rpcMethodKey{}, info.FullMethodName)
}

func (rpcTracker) HandleRPC(ctx context.Context, rs stats.RPCStats) {
	if _, ok := rs.(*stats.End); !ok {
		return
	}
	method, _ := ctx.Value(rpcMethodKey{}).(string)
	slog.Info("rpc completed", "method", method)
}

func (rpcTracker) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return ctx
}

func (rpcTracker) HandleConn(context.Context, stats.ConnStats) {}

func main() {
	flag.Parse()

	addr := fmt.Sprintf(":%d", *port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	defer lis.Close()

	s := grpc.NewServer(grpc.StatsHandler(rpcTracker{}))
	pb.RegisterGreeterServer(s, &server{})

	slog.Info("server started", "address", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
