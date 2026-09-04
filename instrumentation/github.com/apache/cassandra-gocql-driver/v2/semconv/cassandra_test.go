// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryClientTraceAttrs(t *testing.T) {
	tests := []struct {
		name     string
		req      QueryRequest
		expected map[string]interface{}
	}{
		{
			name: "basic select with keyspace and host",
			req: QueryRequest{
				OpName:    "SELECT",
				Statement: "SELECT * FROM users",
				Keyspace:  "my_keyspace",
				Host:      net.ParseIP("127.0.0.1"),
				Port:      9042,
			},
			expected: map[string]interface{}{
				"db.system.name":    "cassandra",
				"db.operation.name": "SELECT",
				"db.query.text":     "SELECT * FROM users",
				"db.namespace":      "my_keyspace",
				"server.address":    "127.0.0.1",
				"server.port":       int64(9042),
			},
		},
		{
			name: "no keyspace and no host",
			req: QueryRequest{
				OpName:    "",
				Statement: "",
			},
			expected: map[string]interface{}{
				"db.system.name": "cassandra",
				"db.query.text":  "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := QueryClientTraceAttrs(tt.req)

			attrMap := make(map[string]interface{})
			for _, attr := range attrs {
				attrMap[string(attr.Key)] = attr.Value.AsInterface()
			}

			require.Len(t, attrMap, len(tt.expected), "attribute count mismatch")
			for key, expectedVal := range tt.expected {
				actualVal, ok := attrMap[key]
				require.True(t, ok, "expected attribute %s not found", key)
				assert.Equal(t, expectedVal, actualVal, "attribute %s value mismatch", key)
			}
		})
	}
}

func TestBatchClientTraceAttrs(t *testing.T) {
	req := BatchRequest{
		Statement: "INSERT INTO a VALUES (1); UPDATE b SET x = 2",
		BatchSize: 2,
		Keyspace:  "batch_ks",
		Host:      net.ParseIP("10.0.0.1"),
		Port:      9042,
	}

	attrs := BatchClientTraceAttrs(req)

	attrMap := make(map[string]interface{})
	for _, attr := range attrs {
		attrMap[string(attr.Key)] = attr.Value.AsInterface()
	}

	assert.Equal(t, "cassandra", attrMap["db.system.name"])
	assert.Equal(t, "BATCH", attrMap["db.operation.name"])
	assert.Equal(t, int64(2), attrMap["db.operation.batch.size"])
	assert.Equal(t, "batch_ks", attrMap["db.namespace"])
	assert.Equal(t, "10.0.0.1", attrMap["server.address"])
	assert.Equal(t, int64(9042), attrMap["server.port"])
}

func TestConnectClientTraceAttrs(t *testing.T) {
	req := ConnectRequest{
		Host: net.ParseIP("10.0.0.2"),
		Port: 9042,
	}

	attrs := ConnectClientTraceAttrs(req)

	attrMap := make(map[string]interface{})
	for _, attr := range attrs {
		attrMap[string(attr.Key)] = attr.Value.AsInterface()
	}

	assert.Equal(t, "cassandra", attrMap["db.system.name"])
	assert.Equal(t, "CONNECT", attrMap["db.operation.name"])
	assert.Equal(t, "10.0.0.2", attrMap["server.address"])
	assert.Equal(t, int64(9042), attrMap["server.port"])
}

func TestConnectClientTraceAttrs_NoHost(t *testing.T) {
	attrs := ConnectClientTraceAttrs(ConnectRequest{})

	attrMap := make(map[string]interface{})
	for _, attr := range attrs {
		attrMap[string(attr.Key)] = attr.Value.AsInterface()
	}

	require.Len(t, attrMap, 2)
	assert.Equal(t, "cassandra", attrMap["db.system.name"])
	assert.Equal(t, "CONNECT", attrMap["db.operation.name"])
}
