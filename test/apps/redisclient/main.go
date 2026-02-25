// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a minimal Redis client for integration testing.
// This client is designed to be instrumented with the otel compile-time tool.
package main

import (
	"context"
	"flag"
	"log"
	"log/slog"

	"github.com/redis/go-redis/v9"
)

var addr = flag.String("addr", "localhost:6379", "The Redis server address")

func main() {
	flag.Parse()

	ctx := context.Background()

	rdb := redis.NewClient(&redis.Options{
		Addr: *addr,
	})
	defer rdb.Close()

	// SET command
	err := rdb.Set(ctx, "testkey", "testvalue", 0).Err()
	if err != nil {
		log.Fatalf("failed to set key: %v", err)
	}
	slog.Info("SET", "key", "testkey", "value", "testvalue")

	// GET command
	val, err := rdb.Get(ctx, "testkey").Result()
	if err != nil {
		log.Fatalf("failed to get key: %v", err)
	}
	slog.Info("GET", "key", "testkey", "value", val)

	// DEL command
	err = rdb.Del(ctx, "testkey").Err()
	if err != nil {
		log.Fatalf("failed to del key: %v", err)
	}
	slog.Info("DEL", "key", "testkey")
}
