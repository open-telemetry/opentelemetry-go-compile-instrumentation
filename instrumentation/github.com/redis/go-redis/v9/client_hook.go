// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package v9

import (
	"context"

	redis "github.com/redis/go-redis/v9"

	"go.opentelemetry.io/otelc/pkg/hook"
	"go.opentelemetry.io/otelc/pkg/runtime"
)

const (
	instrumentationName = "go.opentelemetry.io/otelc/instrumentation/github.com/redis/go-redis/v9"
	instrumentationKey  = "REDIS"
)

// redisClientEnabler controls whether client instrumentation is enabled
type redisClientEnabler struct{}

func (g redisClientEnabler) Enable() bool {
	return runtime.Instrumented(instrumentationKey)
}

var redisEnabler = redisClientEnabler{}

func afterNewRedisClientV9(ictx hook.HookContext, client *redis.Client) {
	client.AddHook(newOtelRedisHook(client.Options().Addr))
}

func afterNewFailOverRedisClientV9(call hook.HookContext, client *redis.Client) {
	client.AddHook(newOtelRedisHook(client.Options().Addr))
}

func afterNewRingClientV9(call hook.HookContext, client *redis.Ring) {
	client.OnNewNode(func(rdb *redis.Client) {
		rdb.AddHook(newOtelRedisHook(rdb.Options().Addr))
	})
	// NewRing builds a shard for every entry in RingOptions.Addrs before this
	// hook runs, and OnNewNode only fires for shards created after it is
	// registered. Without this pass the shards a caller configured up front
	// are never instrumented, which is the common case.
	attachHookToExistingShards(client)
}

// attachHookToExistingShards instruments the shards a ring already holds.
// ForEachShard returns the first error a callback produces; this callback
// cannot fail, so there is nothing to report.
func attachHookToExistingShards(client *redis.Ring) {
	_ = client.ForEachShard(context.Background(), func(_ context.Context, rdb *redis.Client) error {
		rdb.AddHook(newOtelRedisHook(rdb.Options().Addr))
		return nil
	})
}

// afterNewClusterClientV9 relies on OnNewNode alone, and deliberately does not
// need the ForEachShard pass the ring above requires. NewClusterClient creates
// no cluster nodes during construction: its node map starts empty and nodes are
// created lazily on first use (topology load or first command), after this
// callback is registered, so every node passes through OnNewNode. The ring
// needs the extra pass only because NewRing builds its configured shards during
// construction, before the hook runs. If go-redis ever changes to materialise
// cluster nodes eagerly at construction, this would need the same ForEachShard
// treatment; today it does not.
func afterNewClusterClientV9(call hook.HookContext, client *redis.ClusterClient) {
	client.OnNewNode(func(rdb *redis.Client) {
		rdb.AddHook(newOtelRedisHook(rdb.Options().Addr))
	})
}

func afterNewSentinelClientV9(call hook.HookContext, client *redis.SentinelClient) {
	client.AddHook(newOtelRedisHook(client.String()))
}

func afterClientConnV9(call hook.HookContext, client *redis.Conn) {
	client.AddHook(newOtelRedisHook(client.String()))
}
