// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a minimal olivere/elastic v7 client for integration testing.
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
	"syscall"
	"time"

	"github.com/olivere/elastic/v7"
)

var (
	esURL     = flag.String("es-url", "http://localhost:9200", "Elasticsearch URL")
	frontPort = flag.Int("front-port", 8080, "port for HTTP frontend")
)

func main() {
	flag.Parse()

	client, err := elastic.NewSimpleClient(elastic.SetURL(*esURL))
	if err != nil {
		log.Fatalf("failed to create elastic client: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		res, err := client.Search().
			Index("orders").
			Query(elastic.NewMatchAllQuery()).
			Do(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "hits=%d\n", res.TotalHits())
	})
	mux.HandleFunc("/index", func(w http.ResponseWriter, r *http.Request) {
		res, err := client.Index().
			Index("orders").
			Id("1").
			BodyJson(map[string]string{"name": "widget"}).
			Do(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "result=%s id=%s\n", res.Result, res.Id)
	})
	mux.HandleFunc("/delete", func(w http.ResponseWriter, r *http.Request) {
		res, err := client.Delete().
			Index("orders").
			Id("1").
			Do(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "result=%s id=%s\n", res.Result, res.Id)
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
	slog.Info("elasticclient listening", "port", *frontPort, "es", *esURL)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("server shutdown failed: %v", err)
	}
}
