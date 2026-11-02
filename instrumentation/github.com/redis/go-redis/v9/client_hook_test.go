// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package v9

import (
	"context"
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

// Nothing listens on this address, so commands fail at dial time but still
// produce spans.
const (
	unreachableAddr = "127.0.0.1:1"
	unreachableHost = "127.0.0.1"
	unreachablePort = 1
)

func failFastOptions() *redis.Options {
	return &redis.Options{
		Addr:        unreachableAddr,
		DialTimeout: 50 * time.Millisecond,
		MaxRetries:  -1,
	}
}

// newRecordingClient resets global tracer state, so its callers must not run
// in parallel.
func newRecordingClient(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	initOnce = sync.Once{}
	return setupTestTracer(t)
}

func pingAndCollect(t *testing.T, ping func(context.Context) error, sr *tracetest.SpanRecorder) []tracetest.SpanStub {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_ = ping(ctx)

	return tracetest.SpanStubsFromReadOnlySpans(sr.Ended())
}

func assertRedisSpan(t *testing.T, spans []tracetest.SpanStub, wantHost string, wantPort int) {
	t.Helper()
	var clientSpans int
	for _, span := range spans {
		if span.SpanKind != trace.SpanKindClient {
			continue
		}
		clientSpans++
		attrs := make(map[string]any, len(span.Attributes))
		for _, attr := range span.Attributes {
			attrs[string(attr.Key)] = attr.Value.AsInterface()
		}
		assert.Equal(t, "redis", attrs[string(semconv.DBSystemNameKey)])
		if wantHost == "" {
			assert.NotContains(t, attrs, string(semconv.ServerAddressKey))
			assert.NotContains(t, attrs, string(semconv.ServerPortKey))
			continue
		}
		assert.Equal(t, wantHost, attrs[string(semconv.ServerAddressKey)])
		assert.Equal(t, int64(wantPort), attrs[string(semconv.ServerPortKey)])
	}
	require.Positive(t, clientSpans, "expected a Redis client span")
}

func TestAfterNewRedisClientV9_AttachesHook(t *testing.T) {
	sr := newRecordingClient(t)

	client := redis.NewClient(failFastOptions())
	t.Cleanup(func() { _ = client.Close() })

	afterNewRedisClientV9(hooktest.NewMockHookContext(), client)

	spans := pingAndCollect(t, func(ctx context.Context) error {
		return client.Ping(ctx).Err()
	}, sr)
	assertRedisSpan(t, spans, unreachableHost, unreachablePort)
}

func TestAfterNewSentinelClientV9_AttachesHook(t *testing.T) {
	sr := newRecordingClient(t)
	opt := failFastOptions()
	sentinel := redis.NewSentinelClient(opt)
	t.Cleanup(func() { _ = sentinel.Close() })

	afterNewSentinelClientV9(hooktest.NewMockHookContext(opt), sentinel)
	spans := pingAndCollect(t, func(ctx context.Context) error {
		return sentinel.Ping(ctx).Err()
	}, sr)
	assertRedisSpan(t, spans, unreachableHost, unreachablePort)
}

func TestAfterClientConnV9_AttachesHook(t *testing.T) {
	sr := newRecordingClient(t)
	client := redis.NewClient(failFastOptions())
	t.Cleanup(func() { _ = client.Close() })
	conn := client.Conn()
	t.Cleanup(func() { _ = conn.Close() })

	afterClientConnV9(hooktest.NewMockHookContext(client), conn)
	spans := pingAndCollect(t, func(ctx context.Context) error {
		return conn.Ping(ctx).Err()
	}, sr)
	require.Len(t, spans, 1, "Conn should produce exactly one span")
	assertRedisSpan(t, spans, unreachableHost, unreachablePort)
}

func TestAfterNewFailOverRedisClientV9_Unresolved(t *testing.T) {
	sr := newRecordingClient(t)

	client := redis.NewFailoverClient(&redis.FailoverOptions{
		MasterName:    "mymaster",
		SentinelAddrs: []string{unreachableAddr},
		DialTimeout:   50 * time.Millisecond,
		MaxRetries:    -1,
	})
	t.Cleanup(func() { _ = client.Close() })

	afterNewFailOverRedisClientV9(hooktest.NewMockHookContext(), client)

	spans := pingAndCollect(t, func(ctx context.Context) error {
		return client.Ping(ctx).Err()
	}, sr)
	assertRedisSpan(t, spans, "", 0)
}

func TestAfterClientWithTimeoutV9_AttachesHook(t *testing.T) {
	sr := newRecordingClient(t)

	client := redis.NewClient(failFastOptions())
	t.Cleanup(func() { _ = client.Close() })

	afterNewRedisClientV9(hooktest.NewMockHookContext(), client)

	clone := client.WithTimeout(time.Minute)
	t.Cleanup(func() { _ = clone.Close() })

	afterClientWithTimeoutV9(hooktest.NewMockHookContext(), clone)

	spans := pingAndCollect(t, func(ctx context.Context) error {
		return clone.Ping(ctx).Err()
	}, sr)
	require.Len(t, spans, 1, "expected exactly one client span")
	assertRedisSpan(t, spans, unreachableHost, unreachablePort)
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
