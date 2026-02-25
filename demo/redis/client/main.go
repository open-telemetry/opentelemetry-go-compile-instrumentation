// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a Redis client demo for demonstrating OpenTelemetry
// compile-time instrumentation with go-redis/v9.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	requestDelayDuration = 500 * time.Millisecond
)

var (
	addr     = flag.String("addr", "localhost:6379", "Redis server address")
	password = flag.String("password", "", "Redis password")
	db       = flag.Int("db", 0, "Redis database index")
	count    = flag.Int("count", 1, "Number of iterations to run")
	logLevel = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	logger   *slog.Logger
)

func runBasicCommands(ctx context.Context, rdb *redis.Client, iteration int) error {
	prefix := fmt.Sprintf("demo:%d", iteration)

	// SET command
	key := prefix + ":greeting"
	value := fmt.Sprintf("Hello OpenTelemetry #%d", iteration)
	err := rdb.Set(ctx, key, value, 30*time.Second).Err()
	if err != nil {
		logger.Error("SET failed", "key", key, "error", err)
		return err
	}
	logger.Info("SET", "key", key, "value", value)

	// GET command
	result, err := rdb.Get(ctx, key).Result()
	if err != nil {
		logger.Error("GET failed", "key", key, "error", err)
		return err
	}
	logger.Info("GET", "key", key, "value", result)

	// EXISTS command
	exists, err := rdb.Exists(ctx, key).Result()
	if err != nil {
		logger.Error("EXISTS failed", "key", key, "error", err)
		return err
	}
	logger.Info("EXISTS", "key", key, "exists", exists)

	// EXPIRE command
	ok, err := rdb.Expire(ctx, key, 10*time.Second).Result()
	if err != nil {
		logger.Error("EXPIRE failed", "key", key, "error", err)
		return err
	}
	logger.Info("EXPIRE", "key", key, "ok", ok)

	// TTL command
	ttl, err := rdb.TTL(ctx, key).Result()
	if err != nil {
		logger.Error("TTL failed", "key", key, "error", err)
		return err
	}
	logger.Info("TTL", "key", key, "ttl", ttl)

	// DEL command
	deleted, err := rdb.Del(ctx, key).Result()
	if err != nil {
		logger.Error("DEL failed", "key", key, "error", err)
		return err
	}
	logger.Info("DEL", "key", key, "deleted", deleted)

	return nil
}

func runHashCommands(ctx context.Context, rdb *redis.Client, iteration int) error {
	key := fmt.Sprintf("demo:%d:user", iteration)

	// HSET command
	err := rdb.HSet(ctx, key, map[string]interface{}{
		"name":  "Alice",
		"email": "alice@example.com",
		"age":   "30",
	}).Err()
	if err != nil {
		logger.Error("HSET failed", "key", key, "error", err)
		return err
	}
	logger.Info("HSET", "key", key, "fields", "name,email,age")

	// HGET command
	name, err := rdb.HGet(ctx, key, "name").Result()
	if err != nil {
		logger.Error("HGET failed", "key", key, "error", err)
		return err
	}
	logger.Info("HGET", "key", key, "field", "name", "value", name)

	// HGETALL command
	all, err := rdb.HGetAll(ctx, key).Result()
	if err != nil {
		logger.Error("HGETALL failed", "key", key, "error", err)
		return err
	}
	logger.Info("HGETALL", "key", key, "fields", len(all))

	// HDEL command
	hdel, err := rdb.HDel(ctx, key, "age").Result()
	if err != nil {
		logger.Error("HDEL failed", "key", key, "error", err)
		return err
	}
	logger.Info("HDEL", "key", key, "field", "age", "deleted", hdel)

	// Cleanup
	rdb.Del(ctx, key)

	return nil
}

func runListCommands(ctx context.Context, rdb *redis.Client, iteration int) error {
	key := fmt.Sprintf("demo:%d:queue", iteration)

	// LPUSH command
	err := rdb.LPush(ctx, key, "task-1", "task-2", "task-3").Err()
	if err != nil {
		logger.Error("LPUSH failed", "key", key, "error", err)
		return err
	}
	logger.Info("LPUSH", "key", key, "values", "task-1,task-2,task-3")

	// LLEN command
	length, err := rdb.LLen(ctx, key).Result()
	if err != nil {
		logger.Error("LLEN failed", "key", key, "error", err)
		return err
	}
	logger.Info("LLEN", "key", key, "length", length)

	// RPOP command
	val, err := rdb.RPop(ctx, key).Result()
	if err != nil {
		logger.Error("RPOP failed", "key", key, "error", err)
		return err
	}
	logger.Info("RPOP", "key", key, "value", val)

	// LRANGE command
	items, err := rdb.LRange(ctx, key, 0, -1).Result()
	if err != nil {
		logger.Error("LRANGE failed", "key", key, "error", err)
		return err
	}
	logger.Info("LRANGE", "key", key, "items", items)

	// Cleanup
	rdb.Del(ctx, key)

	return nil
}

func runPipeline(ctx context.Context, rdb *redis.Client, iteration int) error {
	prefix := fmt.Sprintf("demo:%d:pipe", iteration)

	// Pipeline - batch multiple commands
	pipe := rdb.Pipeline()
	pipe.Set(ctx, prefix+":a", "value-a", 30*time.Second)
	pipe.Set(ctx, prefix+":b", "value-b", 30*time.Second)
	pipe.Set(ctx, prefix+":c", "value-c", 30*time.Second)
	pipe.Get(ctx, prefix+":a")
	pipe.Get(ctx, prefix+":b")
	pipe.Get(ctx, prefix+":c")

	cmds, err := pipe.Exec(ctx)
	if err != nil {
		logger.Error("Pipeline failed", "error", err)
		return err
	}
	logger.Info("Pipeline executed", "commands", len(cmds))

	// Cleanup
	rdb.Del(ctx, prefix+":a", prefix+":b", prefix+":c")

	return nil
}

func main() {
	defer func() {
		// Wait for OpenTelemetry SDK to flush spans before exit
		time.Sleep(2 * time.Second)
	}()

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

	logger.Info("client starting",
		"redis_address", *addr,
		"redis_db", *db,
		"request_count", *count,
		"log_level", *logLevel)

	// Create Redis client
	rdb := redis.NewClient(&redis.Options{
		Addr:     *addr,
		Password: *password,
		DB:       *db,
	})
	defer rdb.Close()

	ctx := context.Background()

	// Ping to verify connection
	if err := rdb.Ping(ctx).Err(); err != nil {
		logger.Error("failed to connect to Redis", "error", err)
		os.Exit(1)
	}
	logger.Info("connected to Redis", "address", *addr)

	successCount := 0
	failureCount := 0

	for i := 1; i <= *count; i++ {
		logger.Info("starting iteration",
			"iteration", i,
			"total", *count)

		// Run basic string commands
		if err := runBasicCommands(ctx, rdb, i); err != nil {
			failureCount++
			continue
		}

		// Run hash commands
		if err := runHashCommands(ctx, rdb, i); err != nil {
			failureCount++
			continue
		}

		// Run list commands
		if err := runListCommands(ctx, rdb, i); err != nil {
			failureCount++
			continue
		}

		// Run pipeline
		if err := runPipeline(ctx, rdb, i); err != nil {
			failureCount++
			continue
		}

		successCount++

		// Add delay between iterations
		if i < *count {
			time.Sleep(requestDelayDuration)
		}
	}

	logger.Info("client finished",
		"total_iterations", *count,
		"successful", successCount,
		"failed", failureCount)
}
