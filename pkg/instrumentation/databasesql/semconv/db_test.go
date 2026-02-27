// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDbClientRequestTraceAttrs(t *testing.T) {
	tests := []struct {
		name     string
		req      DatabaseSqlRequest
		expected map[string]interface{}
	}{
		{
			name: "basic select query",
			req: DatabaseSqlRequest{
				OpType:     "SELECT",
				Sql:        "SELECT * FROM users WHERE id=?",
				Endpoint:   "127.0.0.1:3306",
				DriverName: "mysql",
				Dsn:        "user:pass@tcp(127.0.0.1:3306)/testdb",
				DbName:     "testdb",
				Params:     []any{1},
			},
			expected: map[string]interface{}{
				"db.system.name":    "mysql",
				"db.operation.name": "SELECT",
				"db.namespace":      "testdb",
				"server.address":    "127.0.0.1",
				"server.port":       int64(3306),
				"network.transport": "tcp",
				"db.query.text":     "SELECT * FROM users WHERE id=?",
			},
		},
		{
			name: "insert query with postgres",
			req: DatabaseSqlRequest{
				OpType:     "INSERT",
				Sql:        "INSERT INTO users (name, email) VALUES (?, ?)",
				Endpoint:   "10.0.0.1:5432",
				DriverName: "postgres",
				Dsn:        "postgres://user:pass@10.0.0.1:5432/mydb",
				DbName:     "mydb",
				Params:     []any{"john", "john@example.com"},
			},
			expected: map[string]interface{}{
				"db.system.name":    "postgresql",
				"db.operation.name": "INSERT",
				"db.namespace":      "mydb",
				"server.address":    "10.0.0.1",
				"server.port":       int64(5432),
				"network.transport": "tcp",
				"db.query.text":     "INSERT INTO users (name, email) VALUES (?, ?)",
			},
		},
		{
			name: "sqlite3 driver",
			req: DatabaseSqlRequest{
				OpType:     "SELECT",
				Sql:        "SELECT * FROM items",
				Endpoint:   "sqlite3",
				DriverName: "sqlite3",
				Dsn:        "file:test.db",
				DbName:     "test",
			},
			expected: map[string]interface{}{
				"db.system.name":    "sqlite",
				"db.operation.name": "SELECT",
				"db.namespace":      "test",
				"server.address":    "sqlite3",
				"network.transport": "tcp",
				"db.query.text":     "SELECT * FROM items",
			},
		},
		{
			name: "unknown driver falls back to other_sql",
			req: DatabaseSqlRequest{
				OpType:     "SELECT",
				Sql:        "SELECT 1",
				Endpoint:   "localhost:9000",
				DriverName: "clickhouse",
				Dsn:        "tcp://localhost:9000/default",
				DbName:     "default",
			},
			expected: map[string]interface{}{
				"db.system.name":    "other_sql",
				"db.operation.name": "SELECT",
				"db.namespace":      "default",
				"server.address":    "localhost",
				"server.port":       int64(9000),
				"network.transport": "tcp",
				"db.query.text":     "SELECT 1",
			},
		},
		{
			name: "empty fields",
			req: DatabaseSqlRequest{
				OpType:     "",
				Sql:        "",
				Endpoint:   "",
				DriverName: "",
				Dsn:        "",
				DbName:     "",
			},
			expected: map[string]interface{}{
				"db.system.name":    "other_sql",
				"db.operation.name": "",
				"db.namespace":      "",
				"server.address":    "",
				"network.transport": "tcp",
				"db.query.text":     "",
			},
		},
		{
			name: "ping operation",
			req: DatabaseSqlRequest{
				OpType:     "ping",
				Sql:        "ping",
				Endpoint:   "localhost:3306",
				DriverName: "mysql",
				Dsn:        "user:pass@tcp(localhost:3306)/testdb",
				DbName:     "testdb",
			},
			expected: map[string]interface{}{
				"db.system.name":    "mysql",
				"db.operation.name": "ping",
				"db.namespace":      "testdb",
				"server.address":    "localhost",
				"server.port":       int64(3306),
				"network.transport": "tcp",
				"db.query.text":     "ping",
			},
		},
		{
			name: "transaction begin",
			req: DatabaseSqlRequest{
				OpType:     "begin",
				Sql:        "START TRANSACTION",
				Endpoint:   "dbhost:3306",
				DriverName: "mysql",
				Dsn:        "user:pass@tcp(dbhost:3306)/prod",
				DbName:     "prod",
			},
			expected: map[string]interface{}{
				"db.system.name":    "mysql",
				"db.operation.name": "begin",
				"db.namespace":      "prod",
				"server.address":    "dbhost",
				"server.port":       int64(3306),
				"network.transport": "tcp",
				"db.query.text":     "START TRANSACTION",
			},
		},
		{
			name: "endpoint without port",
			req: DatabaseSqlRequest{
				OpType:     "SELECT",
				Sql:        "SELECT 1",
				Endpoint:   "dbhost",
				DriverName: "mysql",
				Dsn:        "user:pass@tcp(dbhost)/testdb",
				DbName:     "testdb",
			},
			expected: map[string]interface{}{
				"db.system.name":    "mysql",
				"db.operation.name": "SELECT",
				"db.namespace":      "testdb",
				"server.address":    "dbhost",
				"network.transport": "tcp",
				"db.query.text":     "SELECT 1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := DbClientRequestTraceAttrs(tt.req)
			require.NotNil(t, attrs)
			assert.True(t, len(attrs) > 0, "should return attributes")

			// Convert to map for easier assertion
			attrMap := make(map[string]interface{})
			for _, attr := range attrs {
				attrMap[string(attr.Key)] = attr.Value.AsInterface()
			}

			for key, expectedVal := range tt.expected {
				actualVal, ok := attrMap[key]
				require.True(t, ok, "expected attribute %s not found, got attrs: %v", key, attrMap)
				assert.Equal(t, expectedVal, actualVal, "attribute %s value mismatch", key)
			}
		})
	}
}

func TestDatabaseSqlRequest_Struct(t *testing.T) {
	req := DatabaseSqlRequest{
		OpType:     "SELECT",
		Sql:        "SELECT 1",
		Endpoint:   "localhost:3306",
		DriverName: "mysql",
		Dsn:        "user:pass@tcp(localhost:3306)/db",
		Params:     []any{1, "test"},
		DbName:     "testdb",
	}

	assert.Equal(t, "SELECT", req.OpType)
	assert.Equal(t, "SELECT 1", req.Sql)
	assert.Equal(t, "localhost:3306", req.Endpoint)
	assert.Equal(t, "mysql", req.DriverName)
	assert.Equal(t, "user:pass@tcp(localhost:3306)/db", req.Dsn)
	assert.Equal(t, []any{1, "test"}, req.Params)
	assert.Equal(t, "testdb", req.DbName)
}

func TestDbClientRequestTraceAttrs_ContainsExpectedKeys(t *testing.T) {
	req := DatabaseSqlRequest{
		OpType:     "query",
		Sql:        "SELECT * FROM orders",
		Endpoint:   "db.example.com:5432",
		DriverName: "postgres",
		Dsn:        "postgres://user:pass@db.example.com:5432/orders",
		DbName:     "orders",
	}

	attrs := DbClientRequestTraceAttrs(req)

	// Verify all expected keys are present
	keySet := make(map[string]bool)
	for _, attr := range attrs {
		keySet[string(attr.Key)] = true
	}

	expectedKeys := []string{
		"db.system.name",
		"db.operation.name",
		"db.namespace",
		"server.address",
		"server.port",
		"network.transport",
		"db.query.text",
	}

	for _, key := range expectedKeys {
		assert.True(t, keySet[key], "expected key %s not found in attributes", key)
	}
}
