// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

type RedisRequest struct {
	Endpoint  string
	FullName  string
	Statement string
}

// RedisClientRequestTraceAttrs returns trace attributes for an Redis client request.
func RedisClientRequestTraceAttrs(req RedisRequest) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		semconv.DBSystemNameRedis,
		semconv.DBOperationName(req.FullName),
		semconv.NetworkPeerAddress(req.Endpoint),
		semconv.NetworkTransportTCP,
		semconv.DBQueryText(req.Statement),
	}
	return attrs
}
