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

// Two unreachable endpoints on distinct hosts. Nothing listens on either, so
// every command fails during dial. The hook starts its span before handing off
// to the next hook in the chain, so a failed command still exercises the full
// instrumentation path without needing a Redis server. Distinct hosts let a
// multi-shard test tell one shard's span from another by server.address.
const (
	unreachableAddr  = "127.0.0.1:1"
	unreachableAddr2 = "127.0.0.2:1"
	// The hook splits the endpoint, so only the host reaches server.address.
	unreachableHost  = "127.0.0.1"
	unreachableHost2 = "127.0.0.2"
)

// failFastOptions keeps the dial attempt short and stops go-redis retrying, so
// a test that expects a connection failure does not spend seconds getting one.
func failFastOptions() *redis.Options {
	return &redis.Options{
		Addr:        unreachableAddr,
		DialTimeout: 50 * time.Millisecond,
		MaxRetries:  -1,
	}
}

// ringOptions builds RingOptions for the given shards with the background
// heartbeat pushed far into the future. NewRing otherwise spawns a heartbeat
// goroutine that pings every shard on a 500ms ticker; those background pings
// run through the instrumentation hook and would keep touching the global
// tracer state this package's tests reset between runs, both polluting the span
// recorder and racing the reset. Disabling the ticker leaves each test to drive
// its shards explicitly, which is what it means to assert.
func ringOptions(addrs map[string]string) *redis.RingOptions {
	return &redis.RingOptions{
		Addrs:              addrs,
		DialTimeout:        50 * time.Millisecond,
		MaxRetries:         -1,
		HeartbeatFrequency: time.Hour,
	}
}

// newRecordingClient prepares a span recorder and resets the package-level
// init guard, which otherwise leaks state between tests in this package.
//
// It mutates package-level state (initOnce) and installs a process-global
// TracerProvider, so every test that uses it must run serially. Do not add
// t.Parallel() to those tests: parallel execution would race the shared
// recorder and the init guard and make span capture nondeterministic. A
// longer-term fix is to give initInstrumentation an injectable tracer so the
// reset trick and this constraint both go away.
func newRecordingClient(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	initOnce = *new(sync.Once)
	return setupTestTracer(t)
}

// hostOf returns the host portion of a redis endpoint the same way the
// instrumentation's semconv layer does: the part before the port, or the whole
// string when there is no port. A failover client that has not resolved a
// master yet reports a placeholder addr with no port, so the expected host has
// to be derived rather than hard-coded.
func hostOf(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

// pingAndCollect runs a command that is expected to fail at dial time and
// returns the spans the hook produced along the way.
func pingAndCollect(t *testing.T, ping func(context.Context) error, sr *tracetest.SpanRecorder) []tracetest.SpanStub {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// The error is expected and carries no information worth asserting on. What
	// matters is whether the hook ran, which the recorded spans answer.
	_ = ping(ctx)

	return tracetest.SpanStubsFromReadOnlySpans(sr.Ended())
}

// collectRingSpans fires a command against every shard the ring currently holds
// and returns the spans the hooks produced. Driving each shard directly through
// ForEachShard is deterministic; ring.Ping routes by key and would touch only
// the one shard the key hashes to, which is not what a per-shard test wants to
// assert.
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

// assertRedisSpan checks that the instrumentation produced a client span
// carrying the host the hook was built from.
//
// The assertion deliberately does not pin the span name. Each client type
// issues a different first command once it starts talking to a server, and a
// cluster client sends its topology query before anything the caller asked
// for. What identifies the hook is the endpoint on the span, not the verb.
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

// attrValues flattens a span's attribute values so a test can look for one
// without caring which key holds it.
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

	// Build through the real failover constructor so the test exercises the
	// failover-specific construction path rather than a plain client standing
	// in for it. The sentinel address is unreachable; sentinel discovery fails
	// fast (well under a second, measured) so no Redis or Sentinel server is
	// needed and the test stays deterministic.
	client := redis.NewFailoverClient(&redis.FailoverOptions{
		MasterName:    "mymaster",
		SentinelAddrs: []string{unreachableAddr},
		DialTimeout:   50 * time.Millisecond,
		MaxRetries:    -1,
	})
	t.Cleanup(func() { _ = client.Close() })

	// The hook records whatever Options().Addr holds when it runs. A failover
	// client whose master has not resolved reports a placeholder there, so the
	// expected host comes from the client rather than a literal.
	wantHost := hostOf(client.Options().Addr)
	afterNewFailOverRedisClientV9(hooktest.NewMockHookContext(), client)

	spans := pingAndCollect(t, func(ctx context.Context) error {
		return client.Ping(ctx).Err()
	}, sr)
	assertRedisSpan(t, spans, wantHost)
}

func TestAfterNewRingClientV9_AttachesHookToEachNode(t *testing.T) {
	sr := newRecordingClient(t)

	// Two shards configured up front. NewRing builds both during construction,
	// before afterNewRingClientV9 runs, which is exactly the case OnNewNode
	// alone never reaches. The fix's ForEachShard pass has to instrument both,
	// so a span must appear for each distinct endpoint.
	ring := redis.NewRing(ringOptions(map[string]string{"shard0": unreachableAddr, "shard1": unreachableAddr2}))
	t.Cleanup(func() { _ = ring.Close() })

	afterNewRingClientV9(hooktest.NewMockHookContext(), ring)

	spans := collectRingSpans(t, ring, sr)
	assertRedisSpan(t, spans, unreachableHost)
	assertRedisSpan(t, spans, unreachableHost2)
}

func TestAfterNewRingClientV9_InstrumentsShardAddedViaSetAddrs(t *testing.T) {
	sr := newRecordingClient(t)

	// Start with one shard and install the hook, then add a second shard at
	// runtime the way a caller would. The new shard is created after the hook
	// is registered, so it goes through the retained OnNewNode path rather than
	// the ForEachShard pass. This pins that half of the fix: a refactor that
	// dropped OnNewNode, assuming ForEachShard alone was enough, would silently
	// stop instrumenting dynamically added shards and this test would catch it.
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

	// The sentinel hook labels spans with the client's String() form rather
	// than a bare address, so the expected endpoint comes from the client.
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
