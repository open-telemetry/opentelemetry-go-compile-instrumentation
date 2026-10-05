// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main is a complex demo app combining gin and net/http for e2e testing.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"
)

var (
	frontPort = flag.Int("front-port", 8080, "port for gin frontend")
	backPort  = flag.Int("back-port", 8081, "port for net/http backend")
)

func backendHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "backend says hello")
}

func main() {
	flag.Parse()
	gin.SetMode(gin.ReleaseMode)

	// Start backend (standard net/http)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/backend", backendHandler)
	backendServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", *backPort),
		Handler: mux,
	}
	go func() {
		if err := backendServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("backend error: %v", err)
		}
	}()

	// Start frontend (gin)
	r := gin.New()
	r.GET("/hello", func(c *gin.Context) {
		// Create a request to the backend, explicitly passing the context for propagation
		req, err := http.NewRequestWithContext(c.Request.Context(), "GET", fmt.Sprintf("http://127.0.0.1:%d/api/backend", *backPort), nil)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		c.JSON(http.StatusOK, gin.H{"message": "frontend calling backend", "backend_response": string(body)})
	})

	frontendServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", *frontPort),
		Handler: r,
	}

	go func() {
		if err := frontendServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("frontend error: %v", err)
		}
	}()

	// Wait for shutdown signal
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	backendServer.Shutdown(context.Background())
	frontendServer.Shutdown(context.Background())
}
