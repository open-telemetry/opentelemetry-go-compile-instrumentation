// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"net"
	"strconv"
	"strings"
	"unicode"

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

// OperationName returns the uppercased first token of a SQL statement for use
// as db.operation.name. Empty or whitespace-only input returns "".
func OperationName(query string) string {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return ""
	}
	// Only the first token is wanted, so cut at the first space rather than
	// splitting the whole statement into a slice of every token in it.
	if i := strings.IndexFunc(trimmed, unicode.IsSpace); i != -1 {
		trimmed = trimmed[:i]
	}
	return strings.ToUpper(trimmed)
}

func DbClientRequestTraceAttrs(req DatabaseSqlRequest) []attribute.KeyValue {
	host, portStr, err := net.SplitHostPort(req.Endpoint)
	if err != nil {
		host = req.Endpoint
	}

	// Five attributes are always set, and at most two more are appended below
	// (the port and the database system), so size the slice for all seven up
	// front and skip the regrowth.
	attrs := make([]attribute.KeyValue, 0, 7)
	attrs = append(attrs,
		semconv.DBOperationName(req.OpType),
		semconv.DBNamespace(req.DbName),
		semconv.ServerAddress(host),
		semconv.NetworkTransportTCP,
		semconv.DBQueryText(req.Sql),
	)

	if err == nil {
		if port, convErr := strconv.Atoi(portStr); convErr == nil && port > 0 {
			attrs = append(attrs, semconv.ServerPort(port))
		}
	}

	attrs = append(attrs, dbSystemName(req.DriverName))

	return attrs
}

// SpanName returns the name the database semantic conventions give a client
// span, "{db.operation.name} {target}". The target is db.namespace: the
// instrumentation does not parse the collection out of the query text.
// Without a namespace the name is the operation alone; without an operation,
// the namespace alone, and without either, db.system.name.
func SpanName(req DatabaseSqlRequest) string {
	switch {
	case req.OpType != "" && req.DbName != "":
		return req.OpType + " " + req.DbName
	case req.OpType != "":
		return req.OpType
	case req.DbName != "":
		return req.DbName
	default:
		return dbSystemName(req.DriverName).Value.AsString()
	}
}

// dbSystemName maps a database/sql driver name to db.system.name.
func dbSystemName(driverName string) attribute.KeyValue {
	switch driverName {
	case "mysql", "mariadb":
		return semconv.DBSystemNameMySQL
	case "postgres", "postgresql", "pgx", "lib/pq":
		return semconv.DBSystemNamePostgreSQL
	case "sqlite3", "sqlite":
		return semconv.DBSystemNameSQLite
	case "clickhouse":
		return semconv.DBSystemNameClickHouse
	case "godror", "oracle", "oci8", "go-oci8":
		return semconv.DBSystemNameOracleDB
	case "mssql", "sqlserver":
		return semconv.DBSystemNameMicrosoftSQLServer
	default:
		return semconv.DBSystemNameOtherSQL
	}
}
