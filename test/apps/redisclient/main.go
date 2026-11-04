// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a minimal Redis client for integration testing.
// This client is designed to be instrumented with the otelc compile-time tool.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	addr  = flag.String("addr", "localhost:6379", "The Redis server address")
	mode  = flag.String("mode", "client", "Which client to use: client, sentinel, failover, ring or cluster")
	addr2 = flag.String("addr2", "", "In ring mode, a shard address to add with SetAddrs")
)

func main() {
	flag.Parse()

	ctx := context.Background()

	switch *mode {
	case "client":
		runClient(ctx)
	case "sentinel":
		runSentinel(ctx)
	case "failover":
		runFailover(ctx)
	case "ring":
		runRing(ctx)
	case "cluster":
		runCluster(ctx)
	default:
		log.Fatalf("unknown mode %q", *mode)
	}
}

func runClient(ctx context.Context) {
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
	val, err := rdb.WithTimeout(time.Minute).Get(ctx, "testkey").Result()
	if err != nil {
		log.Fatalf("failed to get key: %v", err)
	}
	slog.Info("GET", "key", "testkey", "value", val)

	conn := rdb.Conn()
	defer func() { _ = conn.Close() }()
	connVal, err := conn.Get(ctx, "connkey").Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		log.Fatalf("failed to get key via Conn: %v", err)
	}
	slog.Info("CONN GET", "key", "connkey", "value", connVal)

	// DEL command
	err = rdb.WithTimeout(time.Minute).WithTimeout(time.Minute).Del(ctx, "testkey").Err()
	if err != nil {
		log.Fatalf("failed to del key: %v", err)
	}
	slog.Info("DEL", "key", "testkey")
}

func runSentinel(ctx context.Context) {
	client := redis.NewSentinelClient(&redis.Options{Addr: *addr})
	defer client.Close()
	if err := client.Ping(ctx).Err(); err != nil {
		log.Fatalf("failed to ping sentinel: %v", err)
	}
}

func runFailover(ctx context.Context) {
	client := redis.NewFailoverClient(&redis.FailoverOptions{MasterName: "mymaster", SentinelAddrs: []string{*addr}})
	defer client.Close()
	before, err := client.Get(ctx, "before").Result()
	if err != nil {
		log.Fatalf("failed to get before failover: %v", err)
	}
	pipe := client.Pipeline()
	pipe.Ping(ctx)
	if _, err := pipe.Exec(ctx); err != nil {
		log.Fatalf("failed to execute pipeline: %v", err)
	}
	conn := client.Conn()
	if err := conn.Ping(ctx).Err(); err != nil {
		log.Fatalf("failed to ping via Conn: %v", err)
	}
	_ = conn.Close()
	control := redis.NewClient(&redis.Options{Addr: *addr})
	defer control.Close()
	if err := control.Do(ctx, "switch").Err(); err != nil {
		log.Fatalf("failed to switch master: %v", err)
	}
	after, err := client.Get(ctx, "after").Result()
	if err != nil {
		log.Fatalf("failed to get after failover: %v", err)
	}
	slog.Info("FAILOVER", "before", before, "after", after)
}

// runRing pings every shard, then adds a shard with SetAddrs and pings again.
// Ring creates both kinds of shard with redis.NewClient.
func runRing(ctx context.Context) {
	ring := redis.NewRing(&redis.RingOptions{
		Addrs:              map[string]string{"shard0": *addr},
		HeartbeatFrequency: time.Hour,
	})
	defer ring.Close()

	if *addr2 != "" {
		ring.SetAddrs(map[string]string{"shard0": *addr, "shard1": *addr2})
	}
	pingShards(ctx, ring)
	slog.Info("RING PING")
}

func pingShards(ctx context.Context, ring *redis.Ring) {
	err := ring.ForEachShard(ctx, func(ctx context.Context, shard *redis.Client) error {
		return shard.Ping(ctx).Err()
	})
	if err != nil {
		log.Fatalf("failed to ping ring shards: %v", err)
	}
}

// runCluster sends one GET through a cluster client. Cluster nodes are
// created lazily with redis.NewClient.
func runCluster(ctx context.Context) {
	cluster := redis.NewClusterClient(&redis.ClusterOptions{
		Addrs: []string{*addr},
	})
	defer cluster.Close()

	err := cluster.Get(ctx, "clusterkey").Err()
	if err != nil && !errors.Is(err, redis.Nil) {
		log.Fatalf("failed to get key via cluster: %v", err)
	}
	slog.Info("CLUSTER GET", "key", "clusterkey")
}
