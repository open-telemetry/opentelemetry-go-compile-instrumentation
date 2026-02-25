// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
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
	attrs := []attribute.KeyValue{
		semconv.DBOperationName(req.OpType),
		semconv.DBNamespace(req.DbName),
		semconv.NetworkPeerAddress(req.Endpoint),
		semconv.NetworkTransportTCP,
		semconv.DBQueryText(req.Sql),
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
