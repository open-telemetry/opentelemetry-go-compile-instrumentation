// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedisClientRequestTraceAttrs(t *testing.T) {
	tests := []struct {
		name     string
		req      RedisRequest
		expected map[string]interface{}
	}{
		{
			name: "basic GET command",
			req: RedisRequest{
				Endpoint:  "localhost:6379",
				FullName:  "get",
				Statement: "get mykey",
			},
			expected: map[string]interface{}{
				"db.system.name":       "redis",
				"db.operation.name":    "get",
				"network.peer.address": "localhost:6379",
				"network.transport":    "tcp",
				"db.query.text":        "get mykey",
			},
		},
		{
			name: "SET command with value",
			req: RedisRequest{
				Endpoint:  "redis.example.com:6380",
				FullName:  "set",
				Statement: "set mykey myvalue",
			},
			expected: map[string]interface{}{
				"db.system.name":       "redis",
				"db.operation.name":    "set",
				"network.peer.address": "redis.example.com:6380",
				"network.transport":    "tcp",
				"db.query.text":        "set mykey myvalue",
			},
		},
		{
			name: "HSET command",
			req: RedisRequest{
				Endpoint:  "127.0.0.1:6379",
				FullName:  "hset",
				Statement: "hset myhash field1 value1",
			},
			expected: map[string]interface{}{
				"db.system.name":       "redis",
				"db.operation.name":    "hset",
				"network.peer.address": "127.0.0.1:6379",
				"network.transport":    "tcp",
				"db.query.text":        "hset myhash field1 value1",
			},
		},
		{
			name: "pipeline command",
			req: RedisRequest{
				Endpoint:  "localhost:6379",
				FullName:  "pipeline",
				Statement: "pipeline get/set/del/...",
			},
			expected: map[string]interface{}{
				"db.system.name":       "redis",
				"db.operation.name":    "pipeline",
				"network.peer.address": "localhost:6379",
				"network.transport":    "tcp",
				"db.query.text":        "pipeline get/set/del/...",
			},
		},
		{
			name: "empty fields",
			req: RedisRequest{
				Endpoint:  "",
				FullName:  "",
				Statement: "",
			},
			expected: map[string]interface{}{
				"db.system.name":       "redis",
				"db.operation.name":    "",
				"network.peer.address": "",
				"network.transport":    "tcp",
				"db.query.text":        "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := RedisClientRequestTraceAttrs(tt.req)

			// Should always return 5 attributes
			require.Len(t, attrs, 5, "should return 5 attributes")

			// Convert to map for easier assertion
			attrMap := make(map[string]interface{})
			for _, attr := range attrs {
				attrMap[string(attr.Key)] = attr.Value.AsInterface()
			}

			for key, expectedVal := range tt.expected {
				actualVal, ok := attrMap[key]
				require.True(t, ok, "expected attribute %s not found", key)
				assert.Equal(t, expectedVal, actualVal, "attribute %s value mismatch", key)
			}
		})
	}
}

func TestRedisClientRequestTraceAttrs_ContainsDBSystemRedis(t *testing.T) {
	req := RedisRequest{
		Endpoint:  "localhost:6379",
		FullName:  "get",
		Statement: "get key",
	}

	attrs := RedisClientRequestTraceAttrs(req)

	found := false
	for _, attr := range attrs {
		if string(attr.Key) == "db.system.name" && attr.Value.AsString() == "redis" {
			found = true
			break
		}
	}
	assert.True(t, found, "should contain db.system.name=redis attribute")
}

func TestRedisClientRequestTraceAttrs_ContainsNetworkTransportTCP(t *testing.T) {
	req := RedisRequest{
		Endpoint:  "localhost:6379",
		FullName:  "get",
		Statement: "get key",
	}

	attrs := RedisClientRequestTraceAttrs(req)

	found := false
	for _, attr := range attrs {
		if string(attr.Key) == "network.transport" && attr.Value.AsString() == "tcp" {
			found = true
			break
		}
	}
	assert.True(t, found, "should contain network.transport=tcp attribute")
}
