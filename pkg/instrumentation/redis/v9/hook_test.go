// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package v9

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func setupTestTracer(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return sr
}

func TestGetRedisV9Statement(t *testing.T) {
	tests := []struct {
		name     string
		cmd      redis.Cmder
		contains string
	}{
		{
			name:     "GET command",
			cmd:      redis.NewCmd(context.Background(), "get", "mykey"),
			contains: "get mykey",
		},
		{
			name:     "SET command with value",
			cmd:      redis.NewCmd(context.Background(), "set", "mykey", "myvalue"),
			contains: "set mykey myvalue",
		},
		{
			name:     "HSET command",
			cmd:      redis.NewCmd(context.Background(), "hset", "myhash", "field1", "value1"),
			contains: "hset myhash field1 value1",
		},
		{
			name:     "DEL command",
			cmd:      redis.NewCmd(context.Background(), "del", "key1", "key2"),
			contains: "del key1 key2",
		},
		{
			name:     "command with nil arg",
			cmd:      redis.NewCmd(context.Background(), "set", nil),
			contains: "set <nil>",
		},
		{
			name:     "command with int arg",
			cmd:      redis.NewCmd(context.Background(), "expire", "mykey", 60),
			contains: "expire mykey 60",
		},
		{
			name:     "command with bool arg true",
			cmd:      redis.NewCmd(context.Background(), "set", "mykey", true),
			contains: "set mykey true",
		},
		{
			name:     "command with bool arg false",
			cmd:      redis.NewCmd(context.Background(), "set", "mykey", false),
			contains: "set mykey false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getRedisV9Statement(tt.cmd)
			assert.Contains(t, result, tt.contains)
		})
	}
}

func TestRedisV9AppendArg(t *testing.T) {
	tests := []struct {
		name     string
		arg      interface{}
		expected string
	}{
		{"nil", nil, "<nil>"},
		{"string", "hello", "hello"},
		{"int", 42, "42"},
		{"int8", int8(8), "8"},
		{"int16", int16(16), "16"},
		{"int32", int32(32), "32"},
		{"int64", int64(64), "64"},
		{"uint", uint(42), "42"},
		{"uint8", uint8(8), "8"},
		{"uint16", uint16(16), "16"},
		{"uint32", uint32(32), "32"},
		{"uint64", uint64(64), "64"},
		{"float32", float32(3.14), "3.14"},
		{"float64", float64(3.14159), "3.14159"},
		{"bool true", true, "true"},
		{"bool false", false, "false"},
		{"bytes valid utf8", []byte("hello"), "hello"},
		{"unsupported type", struct{}{}, "not_support_type"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := redisV9AppendArg(nil, tt.arg)
			assert.Contains(t, string(b), tt.expected)
		})
	}
}

func TestRedisV9AppendArg_Time(t *testing.T) {
	now := time.Now()
	b := redisV9AppendArg(nil, now)
	result := string(b)
	// Should contain RFC3339Nano formatted time
	assert.NotEmpty(t, result)
	// Verify it can be parsed back
	_, err := time.Parse(time.RFC3339Nano, result)
	assert.NoError(t, err)
}

func TestRedisV9AppendArg_InvalidUTF8String(t *testing.T) {
	// Invalid UTF-8 byte sequence
	invalidStr := string([]byte{0xff, 0xfe, 0xfd})
	b := redisV9AppendArg(nil, invalidStr)
	result := string(b)
	assert.Equal(t, "<string>", result)
}

func TestRedisV9AppendArg_InvalidUTF8Bytes(t *testing.T) {
	// Invalid UTF-8 byte sequence
	invalidBytes := []byte{0xff, 0xfe, 0xfd}
	b := redisV9AppendArg(nil, invalidBytes)
	result := string(b)
	assert.Equal(t, "<byte>", result)
}

func TestNewOtelRedisHook(t *testing.T) {
	hook := newOtelRedisHook("localhost:6379")
	assert.NotNil(t, hook)
	assert.Equal(t, "localhost:6379", hook.Addr)
}

func TestProcessHook_CreatesSpan(t *testing.T) {
	initOnce = *new(sync.Once)
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "redis")

	sr := setupTestTracer(t)

	hook := newOtelRedisHook("localhost:6379")
	processHook := hook.ProcessHook(func(ctx context.Context, cmd redis.Cmder) error {
		return nil
	})

	cmd := redis.NewCmd(context.Background(), "get", "mykey")
	err := processHook(context.Background(), cmd)
	assert.NoError(t, err)

	spans := sr.Ended()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, "get", span.Name())

	// Verify attributes
	attrMap := make(map[string]interface{})
	for _, attr := range span.Attributes() {
		attrMap[string(attr.Key)] = attr.Value.AsInterface()
	}
	assert.Equal(t, "redis", attrMap["db.system.name"])
	assert.Equal(t, "get", attrMap["db.operation.name"])
	assert.Equal(t, "localhost", attrMap["server.address"])
	assert.Equal(t, int64(6379), attrMap["server.port"])
}

func TestProcessHook_RecordsError(t *testing.T) {
	initOnce = *new(sync.Once)
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "redis")

	sr := setupTestTracer(t)

	hook := newOtelRedisHook("localhost:6379")
	expectedErr := errors.New("connection refused")
	processHook := hook.ProcessHook(func(ctx context.Context, cmd redis.Cmder) error {
		return expectedErr
	})

	cmd := redis.NewCmd(context.Background(), "get", "mykey")
	err := processHook(context.Background(), cmd)
	assert.Equal(t, expectedErr, err)

	spans := sr.Ended()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, codes.Error, span.Status().Code)
	assert.Contains(t, span.Status().Description, "connection refused")
}

func TestProcessHook_RedisNilNotError(t *testing.T) {
	initOnce = *new(sync.Once)
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "redis")

	sr := setupTestTracer(t)

	hook := newOtelRedisHook("localhost:6379")
	processHook := hook.ProcessHook(func(ctx context.Context, cmd redis.Cmder) error {
		return redis.Nil
	})

	cmd := redis.NewCmd(context.Background(), "get", "nonexistent")
	err := processHook(context.Background(), cmd)
	// redis.Nil must be propagated so callers can detect cache misses via errors.Is
	assert.ErrorIs(t, err, redis.Nil)

	spans := sr.Ended()
	require.Len(t, spans, 1)

	span := spans[0]
	// redis.Nil should NOT mark the span as an error
	assert.Equal(t, codes.Unset, span.Status().Code)
}

func TestProcessHook_Disabled(t *testing.T) {
	initOnce = *new(sync.Once)
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "redis")

	sr := setupTestTracer(t)

	hook := newOtelRedisHook("localhost:6379")
	processHook := hook.ProcessHook(func(ctx context.Context, cmd redis.Cmder) error {
		return nil
	})

	cmd := redis.NewCmd(context.Background(), "get", "mykey")
	err := processHook(context.Background(), cmd)
	assert.NoError(t, err)

	spans := sr.Ended()
	assert.Len(t, spans, 0, "no spans should be created when instrumentation is disabled")
}

func TestProcessPipelineHook_CreatesSpan(t *testing.T) {
	initOnce = *new(sync.Once)
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "redis")

	sr := setupTestTracer(t)

	hook := newOtelRedisHook("localhost:6379")
	pipelineHook := hook.ProcessPipelineHook(func(ctx context.Context, cmds []redis.Cmder) error {
		return nil
	})

	cmds := []redis.Cmder{
		redis.NewCmd(context.Background(), "get", "key1"),
		redis.NewCmd(context.Background(), "set", "key2", "val2"),
		redis.NewCmd(context.Background(), "del", "key3"),
	}
	err := pipelineHook(context.Background(), cmds)
	assert.NoError(t, err)

	spans := sr.Ended()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, "pipeline", span.Name())

	// Verify attributes
	attrMap := make(map[string]interface{})
	for _, attr := range span.Attributes() {
		attrMap[string(attr.Key)] = attr.Value.AsInterface()
	}
	assert.Equal(t, "redis", attrMap["db.system.name"])
	assert.Equal(t, "pipeline", attrMap["db.operation.name"])
}

func TestProcessPipelineHook_TruncatesLongPipeline(t *testing.T) {
	initOnce = *new(sync.Once)
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "redis")

	sr := setupTestTracer(t)

	hook := newOtelRedisHook("localhost:6379")
	pipelineHook := hook.ProcessPipelineHook(func(ctx context.Context, cmds []redis.Cmder) error {
		return nil
	})

	// Create more than 10 commands
	cmds := make([]redis.Cmder, 15)
	for i := range cmds {
		cmds[i] = redis.NewCmd(context.Background(), "get", "key")
	}
	err := pipelineHook(context.Background(), cmds)
	assert.NoError(t, err)

	spans := sr.Ended()
	require.Len(t, spans, 1)
}

func TestProcessPipelineHook_RecordsError(t *testing.T) {
	initOnce = *new(sync.Once)
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "redis")

	sr := setupTestTracer(t)

	hook := newOtelRedisHook("localhost:6379")
	expectedErr := errors.New("pipeline error")
	pipelineHook := hook.ProcessPipelineHook(func(ctx context.Context, cmds []redis.Cmder) error {
		return expectedErr
	})

	cmds := []redis.Cmder{
		redis.NewCmd(context.Background(), "get", "key1"),
	}
	err := pipelineHook(context.Background(), cmds)
	assert.Equal(t, expectedErr, err)

	spans := sr.Ended()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, codes.Error, span.Status().Code)
}

func TestProcessPipelineHook_Disabled(t *testing.T) {
	initOnce = *new(sync.Once)
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "redis")

	sr := setupTestTracer(t)

	hook := newOtelRedisHook("localhost:6379")
	pipelineHook := hook.ProcessPipelineHook(func(ctx context.Context, cmds []redis.Cmder) error {
		return nil
	})

	cmds := []redis.Cmder{
		redis.NewCmd(context.Background(), "get", "key1"),
	}
	err := pipelineHook(context.Background(), cmds)
	assert.NoError(t, err)

	spans := sr.Ended()
	assert.Len(t, spans, 0)
}

func TestDialHook_Success(t *testing.T) {
	hook := newOtelRedisHook("localhost:6379")

	// Create a mock connection using net.Pipe
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	dialHook := hook.DialHook(func(ctx context.Context, network, addr string) (net.Conn, error) {
		return clientConn, nil
	})

	conn, err := dialHook(context.Background(), "tcp", "localhost:6379")
	assert.NoError(t, err)
	assert.NotNil(t, conn)
}

func TestDialHook_Error(t *testing.T) {
	hook := newOtelRedisHook("localhost:6379")
	expectedErr := errors.New("dial tcp: connection refused")

	dialHook := hook.DialHook(func(ctx context.Context, network, addr string) (net.Conn, error) {
		return nil, expectedErr
	})

	conn, err := dialHook(context.Background(), "tcp", "localhost:6379")
	assert.Equal(t, expectedErr, err)
	assert.Nil(t, conn)
}
