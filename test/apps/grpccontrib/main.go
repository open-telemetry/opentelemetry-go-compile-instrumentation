// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main tests otelc gRPC instrumentation when the application also
// registers the otelgrpc stats handlers. grpc-go chains all stats handlers,
// so both otelc and otelgrpc instrument the same RPC.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

var (
	scenario  = flag.String("scenario", "grpc-server", "grpc-server | grpc-client")
	frontPort = flag.Int("front-port", 8080, "port for the front server")
	backPort  = flag.Int("back-port", 50051, "port for the backend server")
)

func main() {
	flag.Parse()

	switch *scenario {
	case "grpc-server":
		srv := grpc.NewServer(grpc.StatsHandler(otelgrpc.NewServerHandler()))
		healthpb.RegisterHealthServer(srv, health.NewServer())
		serveGRPC(*frontPort, srv)
	case "grpc-client":
		back := grpc.NewServer()
		healthpb.RegisterHealthServer(back, health.NewServer())
		serveGRPC(*backPort, back)

		conn, err := grpc.NewClient(fmt.Sprintf("127.0.0.1:%d", *backPort),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithStatsHandler(otelgrpc.NewClientHandler()))
		if err != nil {
			log.Fatal(err)
		}
		defer conn.Close()

		mux := http.NewServeMux()
		mux.HandleFunc("/call", func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()
			if _, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{}); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			fmt.Fprintln(w, "ok")
		})
		go func() {
			if err := http.ListenAndServe(fmt.Sprintf(":%d", *frontPort), mux); err != nil {
				log.Fatal(err)
			}
		}()
	default:
		log.Fatalf("unknown scenario %q", *scenario)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
}

func serveGRPC(port int, srv *grpc.Server) {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatalf("failed to listen on port %d: %v", port, err)
	}
	go func() {
		if err := srv.Serve(ln); err != nil {
			log.Fatalf("server failed: %v", err)
		}
	}()
}
