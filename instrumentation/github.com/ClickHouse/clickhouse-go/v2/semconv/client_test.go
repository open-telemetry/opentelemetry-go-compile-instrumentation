// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

func TestOperationName(t *testing.T) {
	tests := []struct {
		query string
		want  string
	}{
		{"SELECT 1", "SELECT"},
		{"select count() from t", "SELECT"},
		{"  \n\tINSERT INTO t VALUES", "INSERT"},
		{"INSERT INTO t(x) VALUES", "INSERT"},
		{"(SELECT 1) UNION ALL (SELECT 2)", "SELECT"},
		{"/* app=api */ SELECT 1", "SELECT"},
		{"-- note\nALTER TABLE t DELETE WHERE 1", "ALTER"},
		{"# note\nOPTIMIZE TABLE t", "OPTIMIZE"},
		{"WITH x AS (SELECT 1) SELECT * FROM x", "WITH"},
		{"SYSTEM FLUSH LOGS", "SYSTEM"},
		{"", ""},
		{"   ", ""},
		{"/* unterminated", ""},
		{"-- only a comment", ""},
		{"42", ""},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, OperationName(tt.query), "query %q", tt.query)
	}
}

func TestSpanName(t *testing.T) {
	assert.Equal(t, "SELECT db", SpanName("SELECT", "db"))
	assert.Equal(t, "SELECT", SpanName("SELECT", ""))
	assert.Equal(t, "db", SpanName("", "db"))
	assert.Equal(t, "clickhouse", SpanName("", ""))
}

func TestServerFromAddrs(t *testing.T) {
	assert.Equal(t, Server{Address: "ch.example", Port: 9000, Namespace: "db"},
		ServerFromAddrs([]string{"ch.example:9000"}, "db"))
	assert.Equal(t, Server{Address: "::1", Port: 9440},
		ServerFromAddrs([]string{"[::1]:9440"}, ""))
	assert.Equal(t, Server{Address: "ch.example"},
		ServerFromAddrs([]string{"ch.example"}, ""))
	assert.Equal(t, Server{Namespace: "db"},
		ServerFromAddrs([]string{"a:9000", "b:9000"}, "db"), "several addresses: the one used is unknown")
	assert.Equal(t, Server{}, ServerFromAddrs(nil, ""))
}

func TestClientTraceAttrs(t *testing.T) {
	got := ClientTraceAttrs(Request{
		Operation: "INSERT",
		Query:     "INSERT INTO t",
		Server:    Server{Address: "ch.example", Port: 9000, Namespace: "db"},
	})
	assert.ElementsMatch(t, []attribute.KeyValue{
		semconv.DBSystemNameClickHouse,
		semconv.DBOperationName("INSERT"),
		semconv.DBQueryText("INSERT INTO t"),
		semconv.DBNamespace("db"),
		semconv.ServerAddress("ch.example"),
		semconv.ServerPort(9000),
	}, got)

	assert.Equal(t, []attribute.KeyValue{semconv.DBSystemNameClickHouse}, ClientTraceAttrs(Request{}))
}

func TestErrorAttrs(t *testing.T) {
	assert.Equal(t, []attribute.KeyValue{semconv.ErrorTypeOther}, ErrorAttrs(0))
	assert.Equal(t, []attribute.KeyValue{
		semconv.ErrorTypeKey.String("60"),
		semconv.DBResponseStatusCode("60"),
	}, ErrorAttrs(60))
}
