// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"net"

	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

// QueryRequest describes a single CQL query observation for building trace attributes.
type QueryRequest struct {
	OpName    string
	Statement string
	Keyspace  string
	Host      net.IP
	Port      int
}

// QueryClientTraceAttrs returns trace attributes for a single CQL query.
func QueryClientTraceAttrs(req QueryRequest) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		semconv.DBSystemNameCassandra,
		semconv.DBQueryText(req.Statement),
	}
	if req.OpName != "" {
		attrs = append(attrs, semconv.DBOperationName(req.OpName))
	}
	if req.Keyspace != "" {
		attrs = append(attrs, semconv.DBNamespace(req.Keyspace))
	}
	if len(req.Host) > 0 {
		attrs = append(attrs, semconv.ServerAddress(req.Host.String()))
	}
	if req.Port > 0 {
		attrs = append(attrs, semconv.ServerPort(req.Port))
	}
	return attrs
}

// BatchRequest describes a batch of CQL statements for building trace attributes.
type BatchRequest struct {
	Statement string
	BatchSize int
	Keyspace  string
	Host      net.IP
	Port      int
}

// BatchClientTraceAttrs returns trace attributes for a CQL batch.
func BatchClientTraceAttrs(req BatchRequest) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		semconv.DBSystemNameCassandra,
		semconv.DBOperationName("BATCH"),
		semconv.DBQueryText(req.Statement),
		semconv.DBOperationBatchSize(req.BatchSize),
	}
	if req.Keyspace != "" {
		attrs = append(attrs, semconv.DBNamespace(req.Keyspace))
	}
	if len(req.Host) > 0 {
		attrs = append(attrs, semconv.ServerAddress(req.Host.String()))
	}
	if req.Port > 0 {
		attrs = append(attrs, semconv.ServerPort(req.Port))
	}
	return attrs
}

// ConnectRequest describes a connection attempt for building trace attributes.
type ConnectRequest struct {
	Host net.IP
	Port int
}

// ConnectClientTraceAttrs returns trace attributes for a connection attempt.
func ConnectClientTraceAttrs(req ConnectRequest) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		semconv.DBSystemNameCassandra,
		semconv.DBOperationName("CONNECT"),
	}
	if len(req.Host) > 0 {
		attrs = append(attrs, semconv.ServerAddress(req.Host.String()))
	}
	if req.Port > 0 {
		attrs = append(attrs, semconv.ServerPort(req.Port))
	}
	return attrs
}
