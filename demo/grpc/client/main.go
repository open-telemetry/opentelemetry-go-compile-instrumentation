// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"io"
	"log"
	"time"

	pb "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/demo/grpc/server/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	addr   = flag.String("addr", "localhost:50051", "the address to connect to")
	name   = flag.String("name", "world", "Name to greet")
	stream = flag.Bool("stream", false, "Use streaming RPC")
)

func main() {
	flag.Parse()

	// Set up a connection to the server
	conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	c := pb.NewGreeterClient(conn)

	// Contact the server and print out its response
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if *stream {
		// Streaming RPC
		streamClient, err := c.SayHelloStream(ctx)
		if err != nil {
			log.Fatalf("could not create stream: %v", err)
		}

		// Send multiple requests
		for i := 0; i < 5; i++ {
			req := &pb.HelloRequest{Name: *name + " " + time.Now().Format("15:04:05")}
			if err := streamClient.Send(req); err != nil {
				log.Fatalf("could not send: %v", err)
			}
			log.Printf("Sent: %v", req.GetName())
			time.Sleep(500 * time.Millisecond)
		}

		// Close sending side
		if err := streamClient.CloseSend(); err != nil {
			log.Fatalf("could not close send: %v", err)
		}

		// Receive responses
		for {
			reply, err := streamClient.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				log.Fatalf("could not receive: %v", err)
			}
			log.Printf("Stream response: %s", reply.GetMessage())
		}
	} else {
		// Unary RPC
		r, err := c.SayHello(ctx, &pb.HelloRequest{Name: *name})
		if err != nil {
			log.Fatalf("could not greet: %v", err)
		}
		log.Printf("Greeting: %s", r.GetMessage())
	}
}
