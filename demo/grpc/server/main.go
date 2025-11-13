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
	"log"
	"net"
	"os"
	"time"

	pb "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/demo/grpc/server/pb"
	"google.golang.org/grpc"
)

var port = flag.Int("port", 50051, "The server port")

type server struct {
	pb.UnimplementedGreeterServer
}

func (s *server) SayHello(ctx context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	log.Printf("Received: %v", in.GetName())
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
		log.Printf("Received stream: %v", in.GetName())
		if err := stream.Send(&pb.HelloReply{Message: "Hello " + in.GetName()}); err != nil {
			return err
		}
	}
}

func (s *server) Shutdown(ctx context.Context, in *pb.ShutdownRequest) (*pb.ShutdownReply, error) {
	log.Printf("Shutdown request received")
	go func() {
		time.Sleep(100 * time.Millisecond) // Give time to send response
		os.Exit(0)
	}()
	return &pb.ShutdownReply{Message: "Server is shutting down"}, nil
}

func main() {
	flag.Parse()
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterGreeterServer(s, &server{})
	log.Printf("server listening at %v", lis.Addr())
	log.Printf("server started")
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
