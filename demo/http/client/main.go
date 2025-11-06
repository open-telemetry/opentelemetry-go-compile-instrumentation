// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"
)

const (
	defaultTimeout       = 10 * time.Second
	requestDelayDuration = 500 * time.Millisecond
)

var (
	addr     = flag.String("addr", "http://localhost:8080", "the server address to connect to")
	name     = flag.String("name", "world", "Name to greet")
	method   = flag.String("method", "GET", "HTTP method to use (GET or POST)")
	count    = flag.Int("count", 1, "Number of requests to send")
	logLevel = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	shutdown = flag.Bool("shutdown", false, "Shutdown the server")
	logger   *slog.Logger
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

func makeRequest(ctx context.Context, client *http.Client, requestMethod, targetURL, name string) error {
	var req *http.Request
	var err error

	switch requestMethod {
	case "POST":
		reqBody := GreetRequest{Name: name}
		jsonData, marshalErr := json.Marshal(reqBody)
		if marshalErr != nil {
			return fmt.Errorf("could not marshal request: %w", marshalErr)
		}

		req, err = http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewBuffer(jsonData))
		if err != nil {
			return fmt.Errorf("could not create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		logger.Debug("created POST request", "name", name, "url", targetURL)
	case "GET":
		getURL := fmt.Sprintf("%s?name=%s", targetURL, name)
		req, err = http.NewRequestWithContext(ctx, "GET", getURL, nil)
		if err != nil {
			return fmt.Errorf("could not create request: %w", err)
		}
		logger.Debug("created GET request", "name", name, "url", getURL)
	default:
		return fmt.Errorf("unsupported HTTP method: %s", requestMethod)
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("could not send request: %w", err)
	}
	defer resp.Body.Close()
	duration := time.Since(start)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("could not read response: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var response GreetResponse
		if unmarshalErr := json.Unmarshal(body, &response); unmarshalErr != nil {
			return fmt.Errorf("could not unmarshal response: %w", unmarshalErr)
		}
		logger.Info("request successful",
			"method", requestMethod,
			"name", name,
			"message", response.Message,
			"status_code", resp.StatusCode,
			"duration_ms", duration.Milliseconds())
	case http.StatusInternalServerError, http.StatusServiceUnavailable, http.StatusRequestTimeout:
		var errResponse ErrorResponse
		if unmarshalErr := json.Unmarshal(body, &errResponse); unmarshalErr != nil {
			return fmt.Errorf("server error (status %d): %s", resp.StatusCode, string(body))
		}
		logger.Warn("server returned error",
			"method", requestMethod,
			"name", name,
			"error", errResponse.Error,
			"status_code", resp.StatusCode,
			"duration_ms", duration.Milliseconds())
	default:
		return fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
	}

	return nil
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
	slog.SetDefault(logger)

	client := &http.Client{
		Timeout: defaultTimeout,
	}

	ctx := context.Background()
	url := *addr + "/greet"
	if *shutdown {
		url = *addr + "/shutdown"
	}
	logger.Info("client starting",
		"server_address", *addr,
		"method", *method,
		"request_count", *count,
		"log_level", *logLevel)

	successCount := 0
	failureCount := 0

	for i := range *count {
		requestName := *name
		if *count > 1 {
			requestName = fmt.Sprintf("%s-%d", *name, i+1)
		}

		logger.Info("sending request",
			"request_number", i+1,
			"total_requests", *count,
			"method", *method,
			"name", requestName)

		if err := makeRequest(ctx, client, *method, url, requestName); err != nil {
			logger.Error("request failed",
				"request_number", i+1,
				"error", err)
			failureCount++
			// Continue with other requests instead of failing immediately
			continue
		}
		successCount++

		// Add a small delay between requests when sending multiple
		if i < *count-1 {
			time.Sleep(requestDelayDuration)
		}
	}

	logger.Info("client finished",
		"total_requests", *count,
		"successful", successCount,
		"failed", failureCount)
}
