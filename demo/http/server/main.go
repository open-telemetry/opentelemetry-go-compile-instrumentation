// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"time"
)

const (
	faultTypeCount      = 3
	timeoutDelaySeconds = 5
)

var (
	port           = flag.Int("port", 8080, "The server port")
	faultRate      = flag.Float64("fault-rate", 0.1, "Fault injection rate (0.0-1.0)")
	maxLatency     = flag.Int("max-latency", 500, "Maximum artificial latency in milliseconds")
	disableFaults  = flag.Bool("no-faults", false, "Disable fault injection")
	disableLatency = flag.Bool("no-latency", false, "Disable artificial latency")
	logLevel       = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	logger         *slog.Logger
)

type GreetRequest struct {
	Name string `json:"name"`
}

type GreetResponse struct {
	Message string `json:"message"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

// addRandomLatency adds artificial latency to simulate network/processing delays
func addRandomLatency() {
	if *disableLatency {
		return
	}
	latency := time.Duration(rand.IntN(*maxLatency)) * time.Millisecond
	logger.Debug("adding artificial latency", "latency_ms", latency.Milliseconds())
	time.Sleep(latency)
}

// shouldInjectFault determines if a fault should be injected based on the fault rate
func shouldInjectFault() bool {
	if *disableFaults {
		return false
	}
	return rand.Float64() < *faultRate
}

func greetHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Add random latency
	addRandomLatency()

	// Random fault injection
	if shouldInjectFault() {
		faultType := rand.IntN(faultTypeCount)
		switch faultType {
		case 0:
			logger.Warn("injecting fault",
				"fault_type", "internal_server_error",
				"status_code", http.StatusInternalServerError,
				"method", r.Method,
				"path", r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
			if err := json.NewEncoder(w).Encode(ErrorResponse{Error: "internal server error"}); err != nil {
				logger.Error("failed to encode error response", "error", err)
			}
		case 1:
			logger.Warn("injecting fault",
				"fault_type", "service_unavailable",
				"status_code", http.StatusServiceUnavailable,
				"method", r.Method,
				"path", r.URL.Path)
			w.WriteHeader(http.StatusServiceUnavailable)
			if err := json.NewEncoder(w).Encode(ErrorResponse{Error: "service temporarily unavailable"}); err != nil {
				logger.Error("failed to encode error response", "error", err)
			}
		case 2:
			logger.Warn("injecting fault",
				"fault_type", "timeout",
				"status_code", http.StatusRequestTimeout,
				"method", r.Method,
				"path", r.URL.Path,
				"delay_seconds", timeoutDelaySeconds)
			time.Sleep(timeoutDelaySeconds * time.Second)
			w.WriteHeader(http.StatusRequestTimeout)
			if err := json.NewEncoder(w).Encode(ErrorResponse{Error: "request timeout"}); err != nil {
				logger.Error("failed to encode error response", "error", err)
			}
		}
		return
	}

	var name string
	switch r.Method {
	case http.MethodPost:
		var req GreetRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			logger.Error("failed to decode request body",
				"error", err,
				"method", r.Method,
				"path", r.URL.Path)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		name = req.Name
		logger.Info("received request",
			"method", "POST",
			"name", name,
			"path", r.URL.Path)
	case http.MethodGet:
		name = r.URL.Query().Get("name")
		if name == "" {
			name = "world"
		}
		logger.Info("received request",
			"method", "GET",
			"name", name,
			"path", r.URL.Path)
	default:
		logger.Warn("method not allowed",
			"method", r.Method,
			"path", r.URL.Path)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	response := GreetResponse{
		Message: "Hello " + name,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		logger.Error("failed to encode response",
			"error", err,
			"name", name)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		logger.Error("failed to encode health response", "error", err)
	}
}

func shutdownHandler(w http.ResponseWriter, _ *http.Request) {
	go func() {
		time.Sleep(time.Second) // Give time for spans to be exported
		os.Exit(0)
	}()
}

func main() {
	flag.Parse()

	// Initialize logger with appropriate level
	var level slog.Level
	switch *logLevel {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: level,
	}
	logger = slog.New(slog.NewJSONHandler(os.Stdout, opts))

	http.HandleFunc("/greet", greetHandler)
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/shutdown", shutdownHandler)

	addr := fmt.Sprintf(":%d", *port)
	logger.Info("server starting",
		"address", addr,
		"fault_rate", *faultRate,
		"max_latency_ms", *maxLatency,
		"faults_disabled", *disableFaults,
		"latency_disabled", *disableLatency,
		"log_level", *logLevel)

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		logger.Error("server failed to listen", "error", err)
		os.Exit(1)
	}
	defer listener.Close()
	logger.Info("server started", "address", listener.Addr())
	if err := http.Serve(listener, nil); err != nil {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}
