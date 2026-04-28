// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package v9

import (
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/shared"
	redis "github.com/redis/go-redis/v9"
)

const (
	instrumentationName = "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/redis"
	instrumentationKey  = "REDIS"
)

// redisClientEnabler controls whether client instrumentation is enabled
type redisClientEnabler struct{}

func (g redisClientEnabler) Enable() bool {
	return shared.Instrumented(instrumentationKey)
}

var redisEnabler = redisClientEnabler{}

func afterNewRedisClientV9(ictx inst.HookContext, client *redis.Client) {
	client.AddHook(newOtelRedisHook(client.Options().Addr))
}

func afterNewFailOverRedisClientV9(call inst.HookContext, client *redis.Client) {
	client.AddHook(newOtelRedisHook(client.Options().Addr))
}

func afterNewRingClientV9(call inst.HookContext, client *redis.Ring) {
	client.OnNewNode(func(rdb *redis.Client) {
		rdb.AddHook(newOtelRedisHook(rdb.Options().Addr))
	})
}

func afterNewClusterClientV9(call inst.HookContext, client *redis.ClusterClient) {
	client.OnNewNode(func(rdb *redis.Client) {
		rdb.AddHook(newOtelRedisHook(rdb.Options().Addr))
	})
}

func afterNewSentinelClientV9(call inst.HookContext, client *redis.SentinelClient) {
	client.AddHook(newOtelRedisHook(client.String()))
}

func afterClientConnV9(call inst.HookContext, client *redis.Conn) {
	client.AddHook(newOtelRedisHook(client.String()))
}
