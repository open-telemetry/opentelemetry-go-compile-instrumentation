// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package v9

import (
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

// Ring and ClusterClient use redis.NewClient by default to create their node
// clients (unless overridden via RingOptions.NewClient or ClusterOptions.NewClient),
// so this hook instruments them too.
func afterNewRedisClientV9(ictx hook.HookContext, client *redis.Client) {
	if client == nil {
		return
	}
	client.AddHook(newOtelRedisHook(client.Options().Addr))
}

func afterNewFailOverRedisClientV9(call hook.HookContext, client *redis.Client) {
	if client == nil {
		return
	}
	// The hook cannot determine which pooled connection a command uses.
	client.AddHook(newOtelRedisHook(""))
}

func afterNewSentinelClientV9(call hook.HookContext, client *redis.SentinelClient) {
	if client == nil {
		return
	}
	var addr string
	if call != nil {
		if opt, ok := call.GetParam(0).(*redis.Options); ok && opt != nil {
			addr = opt.Addr
		}
	}
	client.AddHook(newOtelRedisHook(addr))
}

func afterClientConnV9(call hook.HookContext, conn *redis.Conn) {
	if conn == nil {
		return
	}
	var addr string
	if call != nil {
		if parent, ok := call.GetParam(0).(*redis.Client); ok && parent != nil {
			addr = parent.Options().Addr
		}
	}
	conn.AddHook(newOtelRedisHook(addr))
}

func afterClientWithTimeoutV9(ctx hook.HookContext, client *redis.Client) {
	afterNewRedisClientV9(ctx, client)
}
