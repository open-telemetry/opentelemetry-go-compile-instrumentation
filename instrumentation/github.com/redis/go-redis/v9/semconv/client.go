// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"net"
	"strconv"

	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

type RedisRequest struct {
	Endpoint  string
	FullName  string
	Statement string
}

// redisClientRequestAttrCap is the most attributes RedisClientRequestTraceAttrs
// can return: five required fields plus server.port.
const redisClientRequestAttrCap = 6

// RedisClientRequestTraceAttrs returns trace attributes for a Redis client request.
func RedisClientRequestTraceAttrs(req RedisRequest) []attribute.KeyValue {
	host, portStr, err := net.SplitHostPort(req.Endpoint)
	if err != nil {
		host = req.Endpoint
	}

	attrs := make([]attribute.KeyValue, 0, redisClientRequestAttrCap)
	attrs = append(attrs,
		semconv.DBSystemNameRedis,
		semconv.DBOperationName(req.FullName),
		semconv.ServerAddress(host),
		semconv.NetworkTransportTCP,
		semconv.DBQueryText(req.Statement),
	)

	if err == nil {
		if port, convErr := strconv.Atoi(portStr); convErr == nil && port > 0 {
			attrs = append(attrs, semconv.ServerPort(port))
		}
	}

	return attrs
}
