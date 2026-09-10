// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpanName(t *testing.T) {
	tests := []struct {
		operation, index, want string
	}{
		{"search", "orders", "search orders"},
		{"search", "", "search"},
		{"", "orders", "orders"},
		{"", "", "elasticsearch"},
		{"  index  ", "  logs  ", "index logs"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, SpanName(tt.operation, tt.index))
	}
}

func TestClientTraceAttrs(t *testing.T) {
	attrs := ClientTraceAttrs(Request{
		Method:    "POST",
		Path:      "orders/_search?pretty=true",
		Operation: "search",
		Index:     "orders",
	})

	got := make(map[string]interface{}, len(attrs))
	for _, a := range attrs {
		got[string(a.Key)] = a.Value.AsInterface()
	}

	require.Equal(t, "elasticsearch", got["db.system.name"])
	require.Equal(t, "search", got["db.operation.name"])
	require.Equal(t, "orders", got["db.collection.name"])
	require.Equal(t, "POST", got["http.request.method"])
	require.Equal(t, "/orders/_search", got["url.path"])
	require.Equal(t, "tcp", got["network.transport"])
}

func TestClientTraceAttrs_UnescapesPath(t *testing.T) {
	attrs := ClientTraceAttrs(Request{
		Method:    "POST",
		Path:      "/orders%2Clogs/_search",
		Operation: "search",
		Index:     "orders,logs",
	})
	got := make(map[string]interface{}, len(attrs))
	for _, a := range attrs {
		got[string(a.Key)] = a.Value.AsInterface()
	}
	assert.Equal(t, "/orders,logs/_search", got["url.path"])
	assert.Equal(t, "orders,logs", got["db.collection.name"])
}

func TestClientTraceAttrs_OmitsEmptyOptional(t *testing.T) {
	attrs := ClientTraceAttrs(Request{})
	got := make(map[string]interface{}, len(attrs))
	for _, a := range attrs {
		got[string(a.Key)] = a.Value.AsInterface()
	}
	assert.Equal(t, "elasticsearch", got["db.system.name"])
	assert.Equal(t, "tcp", got["network.transport"])
	_, hasOp := got["db.operation.name"]
	_, hasIndex := got["db.collection.name"]
	_, hasMethod := got["http.request.method"]
	_, hasPath := got["url.path"]
	assert.False(t, hasOp)
	assert.False(t, hasIndex)
	assert.False(t, hasMethod)
	assert.False(t, hasPath)
}
