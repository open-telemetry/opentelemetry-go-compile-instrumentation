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
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/otelc/test/shared/grpcpb"
	"go.opentelemetry.io/otelc/test/shared/grpcpb/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/stats"
)

var (
	port  = flag.Int("port", 50051, "The server port")
	drain = flag.Duration("drain", 0, "If set, handle SIGINT and SIGTERM with signal.NotifyContext, "+
		"stop listening for them as soon as one arrives, then keep running for this long before exiting")
)

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
	slog.Info(grpcpb.RPCCompletedLog(method))
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
	if *drain > 0 {
		// Register before serving, so the handler is in place by the time a
		// test can complete an RPC and send the signal.
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		go serve(s, lis)
		waitAndDrain(ctx, stop, *drain)
		s.Stop()
		return
	}
	serve(s, lis)
}

func serve(s *grpc.Server, lis net.Listener) {
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

// waitAndDrain follows a common graceful shutdown pattern: it calls stop() as
// soon as the signal arrives, which restores the default signal disposition,
// and then drains for d. A second shutdown signal sent during the drain would
// kill the process.
func waitAndDrain(ctx context.Context, stop context.CancelFunc, d time.Duration) {
	<-ctx.Done()
	stop()
	slog.Info("draining")
	time.Sleep(d)
	slog.Info("drained")
}
