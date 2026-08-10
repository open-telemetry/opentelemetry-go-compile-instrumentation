// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a minimal Redis client for integration testing.
// This client is designed to be instrumented with the otelc compile-time tool.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"

	"github.com/redis/go-redis/v9"
)

var addr = flag.String("addr", "localhost:6379", "The Redis server address")

func run() error {
	flag.Parse()

	ctx := context.Background()

	rdb := redis.NewClient(&redis.Options{
		Addr: *addr,
	})
	defer rdb.Close()

	// SET command
	err := rdb.Set(ctx, "testkey", "testvalue", 0).Err()
	if err != nil {
		return fmt.Errorf("failed to set key: %w", err)
	}
	slog.Info("SET", "key", "testkey", "value", "testvalue")

	// GET command
	val, err := rdb.Get(ctx, "testkey").Result()
	if err != nil {
		return fmt.Errorf("failed to get key: %w", err)
	}
	slog.Info("GET", "key", "testkey", "value", val)

	// DEL command
	err = rdb.Del(ctx, "testkey").Err()
	if err != nil {
		return fmt.Errorf("failed to del key: %w", err)
	}
	slog.Info("DEL", "key", "testkey")
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Printf("error: %v\n", err)
		os.Exit(1)
	}
}
