// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"net"
	"strconv"

	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

type DatabaseSqlRequest struct {
	OpType     string
	Sql        string
	Endpoint   string
	DriverName string
	Dsn        string
	Params     []any
	DbName     string
}

func DbClientRequestTraceAttrs(req DatabaseSqlRequest) []attribute.KeyValue {
	host, portStr, err := net.SplitHostPort(req.Endpoint)
	if err != nil {
		host = req.Endpoint
	}

	attrs := []attribute.KeyValue{
		semconv.DBOperationName(req.OpType),
		semconv.DBNamespace(req.DbName),
		semconv.ServerAddress(host),
		semconv.NetworkTransportTCP,
		semconv.DBQueryText(req.Sql),
	}

	if err == nil {
		if port, convErr := strconv.Atoi(portStr); convErr == nil && port > 0 {
			attrs = append(attrs, semconv.ServerPort(port))
		}
	}

	switch req.DriverName {
	case "mysql":
		attrs = append(attrs, semconv.DBSystemNameMySQL)
	case "postgres":
		attrs = append(attrs, semconv.DBSystemNamePostgreSQL)
	case "sqlite3":
		attrs = append(attrs, semconv.DBSystemNameSQLite)
	default:
		attrs = append(attrs, semconv.DBSystemNameOtherSQL)
	}

	return attrs
}
