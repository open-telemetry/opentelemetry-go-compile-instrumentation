// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package v9

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otelc/pkg/hook/hooktest"
)

// Nothing listens on these, so commands fail at dial time but still produce
// spans. Distinct hosts let a ring test tell its shards apart.
const (
	unreachableAddr  = "127.0.0.1:1"
	unreachableAddr2 = "127.0.0.2:1"
	unreachableHost  = "127.0.0.1"
	unreachableHost2 = "127.0.0.2"
)

func failFastOptions() *redis.Options {
	return &redis.Options{
		Addr:        unreachableAddr,
		DialTimeout: 50 * time.Millisecond,
		MaxRetries:  -1,
	}
}

// ringOptions pushes the heartbeat out so background pings do not add spans.
func ringOptions(addrs map[string]string) *redis.RingOptions {
	return &redis.RingOptions{
		Addrs:              addrs,
		DialTimeout:        50 * time.Millisecond,
		MaxRetries:         -1,
		HeartbeatFrequency: time.Hour,
	}
}

// newRecordingClient resets global tracer state, so its callers must not run
// in parallel.
func newRecordingClient(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	initOnce = sync.Once{}
	return setupTestTracer(t)
}

func hostOf(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

func pingAndCollect(t *testing.T, ping func(context.Context) error, sr *tracetest.SpanRecorder) []tracetest.SpanStub {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_ = ping(ctx)

	return tracetest.SpanStubsFromReadOnlySpans(sr.Ended())
}

// collectRingSpans pings every shard directly; ring.Ping would reach only one.
func collectRingSpans(t *testing.T, ring *redis.Ring, sr *tracetest.SpanRecorder) []tracetest.SpanStub {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_ = ring.ForEachShard(ctx, func(c context.Context, rdb *redis.Client) error {
		_ = rdb.Ping(c).Err()
		return nil
	})

	return tracetest.SpanStubsFromReadOnlySpans(sr.Ended())
}

// assertRedisSpan matches on server.address rather than the span name, since
// the first command differs by client type.
func assertRedisSpan(t *testing.T, spans []tracetest.SpanStub, wantHost string) {
	t.Helper()
	require.NotEmpty(t, spans, "expected the hook to record a span for the command")

	var names []string
	for _, span := range spans {
		names = append(names, span.Name)
		if span.SpanKind != trace.SpanKindClient {
			continue
		}
		for _, attr := range span.Attributes {
			if attr.Key == semconv.ServerAddressKey && attr.Value.AsString() == wantHost {
				assert.Contains(t, attrValues(span), "redis",
					"expected the span to be marked as a redis client span")
				return
			}
		}
	}
	t.Fatalf("no client span carried server.address=%q; recorded spans: %v", wantHost, names)
}

func attrValues(span tracetest.SpanStub) []string {
	values := make([]string, 0, len(span.Attributes))
	for _, attr := range span.Attributes {
		values = append(values, attr.Value.String())
	}
	return values
}

func TestAfterNewRedisClientV9_AttachesHook(t *testing.T) {
	sr := newRecordingClient(t)

	client := redis.NewClient(failFastOptions())
	t.Cleanup(func() { _ = client.Close() })

	afterNewRedisClientV9(hooktest.NewMockHookContext(), client)

	spans := pingAndCollect(t, func(ctx context.Context) error {
		return client.Ping(ctx).Err()
	}, sr)
	assertRedisSpan(t, spans, unreachableHost)
}

func TestAfterNewFailOverRedisClientV9_AttachesHook(t *testing.T) {
	sr := newRecordingClient(t)

	client := redis.NewFailoverClient(&redis.FailoverOptions{
		MasterName:    "mymaster",
		SentinelAddrs: []string{unreachableAddr},
		DialTimeout:   50 * time.Millisecond,
		MaxRetries:    -1,
	})
	t.Cleanup(func() { _ = client.Close() })

	// An unresolved failover client reports a placeholder addr.
	wantHost := hostOf(client.Options().Addr)
	afterNewFailOverRedisClientV9(hooktest.NewMockHookContext(), client)

	spans := pingAndCollect(t, func(ctx context.Context) error {
		return client.Ping(ctx).Err()
	}, sr)
	assertRedisSpan(t, spans, wantHost)
}

func TestAfterNewRingClientV9_AttachesHookToEachNode(t *testing.T) {
	sr := newRecordingClient(t)

	// Both shards exist before the hook runs, so only ForEachShard reaches them.
	ring := redis.NewRing(ringOptions(map[string]string{"shard0": unreachableAddr, "shard1": unreachableAddr2}))
	t.Cleanup(func() { _ = ring.Close() })

	afterNewRingClientV9(hooktest.NewMockHookContext(), ring)

	spans := collectRingSpans(t, ring, sr)
	assertRedisSpan(t, spans, unreachableHost)
	assertRedisSpan(t, spans, unreachableHost2)
}

func TestAfterNewRingClientV9_InstrumentsShardAddedViaSetAddrs(t *testing.T) {
	sr := newRecordingClient(t)

	// A shard added after the hook runs must still go through OnNewNode.
	ring := redis.NewRing(ringOptions(map[string]string{"shard0": unreachableAddr}))
	t.Cleanup(func() { _ = ring.Close() })

	afterNewRingClientV9(hooktest.NewMockHookContext(), ring)
	ring.SetAddrs(map[string]string{"shard0": unreachableAddr, "shard1": unreachableAddr2})

	spans := collectRingSpans(t, ring, sr)
	assertRedisSpan(t, spans, unreachableHost2)
}

func TestAfterNewClusterClientV9_RegistersNodeCallback(t *testing.T) {
	sr := newRecordingClient(t)

	cluster := redis.NewClusterClient(&redis.ClusterOptions{
		Addrs:       []string{unreachableAddr},
		DialTimeout: 50 * time.Millisecond,
		MaxRetries:  -1,
	})
	t.Cleanup(func() { _ = cluster.Close() })

	afterNewClusterClientV9(hooktest.NewMockHookContext(), cluster)

	spans := pingAndCollect(t, func(ctx context.Context) error {
		return cluster.Ping(ctx).Err()
	}, sr)
	assertRedisSpan(t, spans, unreachableHost)
}

func TestAfterNewSentinelClientV9_AttachesHook(t *testing.T) {
	sr := newRecordingClient(t)

	sentinel := redis.NewSentinelClient(failFastOptions())
	t.Cleanup(func() { _ = sentinel.Close() })

	wantAddr := sentinel.String()
	afterNewSentinelClientV9(hooktest.NewMockHookContext(), sentinel)

	spans := pingAndCollect(t, func(ctx context.Context) error {
		return sentinel.Ping(ctx).Err()
	}, sr)
	assertRedisSpan(t, spans, wantAddr)
}

func TestAfterClientConnV9_AttachesHook(t *testing.T) {
	sr := newRecordingClient(t)

	client := redis.NewClient(failFastOptions())
	t.Cleanup(func() { _ = client.Close() })

	conn := client.Conn()
	t.Cleanup(func() { _ = conn.Close() })

	wantAddr := conn.String()
	afterClientConnV9(hooktest.NewMockHookContext(), conn)

	spans := pingAndCollect(t, func(ctx context.Context) error {
		return conn.Ping(ctx).Err()
	}, sr)
	assertRedisSpan(t, spans, wantAddr)
}

func TestRedisClientEnabler(t *testing.T) {
	tests := []struct {
		name     string
		setupEnv func(t *testing.T)
		expected bool
	}{
		{
			name: "enabled explicitly",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "redis")
			},
			expected: true,
		},
		{
			name: "disabled explicitly",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "redis")
			},
			expected: false,
		},
		{
			name: "not in enabled list",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp")
			},
			expected: false,
		},
		{
			name: "default enabled when no env set",
			setupEnv: func(t *testing.T) {
				// No environment variables set - should be enabled by default
			},
			expected: true,
		},
		{
			name: "enabled with multiple instrumentations",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp,redis,grpc")
			},
			expected: true,
		},
		{
			name: "disabled with multiple instrumentations",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "redis,grpc")
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupEnv(t)

			enabler := redisClientEnabler{}
			result := enabler.Enable()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestInstrumentationConstants(t *testing.T) {
	assert.Equal(
		t,
		"go.opentelemetry.io/otelc/instrumentation/github.com/redis/go-redis/v9",
		instrumentationName,
	)
	assert.Equal(t, "REDIS", instrumentationKey)
}
