// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main tests otelc net/http client instrumentation when the
// application also wraps the transport with otelhttp. otelhttp starts its own
// client span and injects headers before the base transport runs. otelc must
// step aside for that call and still instrument calls from a plain client.
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

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

var (
	frontPort = flag.Int("front-port", 8080, "port for the front server")
	backPort  = flag.Int("back-port", 8081, "port for the backend server")
)

func main() {
	flag.Parse()

	// Backend: plain net/http server. Its server spans come from otelc only.
	backMux := http.NewServeMux()
	backMux.HandleFunc("/back-otelhttp", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})
	backMux.HandleFunc("/back-plain", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})
	backServer := &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", *backPort),
		Handler: backMux,
	}
	go func() {
		if err := backServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("backend error: %v", err)
		}
	}()

	// One handler, two outbound calls: one through otelhttp's transport
	// wrapper, one through a plain client. Both must carry exactly one client
	// span: otelhttp's for the first call, otelc's for the second.
	otelClient := &http.Client{Transport: otelhttp.NewTransport(&http.Transport{})}
	plainClient := &http.Client{}
	frontMux := http.NewServeMux()
	frontMux.HandleFunc("/call", func(w http.ResponseWriter, r *http.Request) {
		otelReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet,
			fmt.Sprintf("http://127.0.0.1:%d/back-otelhttp", *backPort), nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		resp, err := otelClient.Do(otelReq)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)

		plainReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet,
			fmt.Sprintf("http://127.0.0.1:%d/back-plain", *backPort), nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		resp, err = plainClient.Do(plainReq)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)

		_, _ = io.WriteString(w, "ok")
	})
	frontServer := &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", *frontPort),
		Handler: frontMux,
	}
	go func() {
		if err := frontServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("frontend error: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	_ = backServer.Shutdown(context.Background())
	_ = frontServer.Shutdown(context.Background())
}
