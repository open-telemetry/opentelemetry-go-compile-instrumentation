// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a minimal opensearch-go v5 client for integration testing.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/opensearch-project/opensearch-go/v5"
	"github.com/opensearch-project/opensearch-go/v5/opensearchapi"
)

var (
	osURL     = flag.String("os-url", "http://localhost:9200", "OpenSearch URL")
	frontPort = flag.Int("front-port", 8080, "port for HTTP frontend")
)

func main() {
	flag.Parse()

	// Discovery and health checks bypass (*Client).Request. Disable both in this
	// deterministic fixture so only endpoint-triggered spans are asserted.
	discover := false
	client, err := opensearchapi.NewClient(opensearchapi.Config{
		Client: opensearch.Config{
			Addresses:             []string{*osURL},
			DiscoverNodesOnStart:  &discover,
			HealthCheckMaxRetries: -1,
		},
	})
	if err != nil {
		log.Fatalf("failed to create opensearch client: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		resp, err := client.Search(r.Context(), &opensearchapi.SearchReq{
			Indices:    []string{"orders"},
			BodyReader: strings.NewReader(`{"query":{"match_all":{}}}`),
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "search status=%d\n", resp.Inspect().Response.StatusCode)
	})
	mux.HandleFunc("/index", func(w http.ResponseWriter, r *http.Request) {
		resp, err := client.Doc.Index(r.Context(), opensearchapi.IndexReq{
			Index: "orders",
			ID:    "1",
			Body:  strings.NewReader(`{"title":"widget"}`),
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "id=%s index=%s\n", resp.ID, resp.Index)
	})
	mux.HandleFunc("/delete", func(w http.ResponseWriter, r *http.Request) {
		resp, err := client.Doc.Delete(r.Context(), opensearchapi.DeleteReq{
			Index: "orders",
			ID:    "1",
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "id=%s index=%s\n", resp.ID, resp.Index)
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", *frontPort),
		Handler: mux,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("frontend server failed: %v", err)
		}
	}()
	slog.Info("opensearchclientv5 listening", "port", *frontPort, "os", *osURL)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("server shutdown failed: %v", err)
	}
}
