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
// as db.operation.name. Leading comments are skipped. Empty, whitespace-only
// or comment-only input returns "".
func OperationName(query string) string {
	trimmed := skipLeadingComments(query)
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

// skipLeadingComments trims leading whitespace and any "--" line comments and
// "/* */" block comments before the first token, such as the "-- name: ..."
// line that sqlc puts at the start of every generated query. An unterminated
// comment leaves nothing. Block comments do not nest, and MySQL executable
// comments ("/*! ... */") are skipped like any other block comment.
func skipLeadingComments(query string) string {
	s := strings.TrimSpace(query)
	for {
		switch {
		case strings.HasPrefix(s, "--"):
			end := strings.IndexAny(s, "\r\n")
			if end < 0 {
				return ""
			}
			s = strings.TrimSpace(s[end+1:])
		case strings.HasPrefix(s, "/*"):
			end := strings.Index(s[2:], "*/")
			if end < 0 {
				return ""
			}
			s = strings.TrimSpace(s[2+end+2:])
		default:
			return s
		}
	}
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

	switch req.DriverName {
	case "mysql", "mariadb":
		attrs = append(attrs, semconv.DBSystemNameMySQL)
	case "postgres", "postgresql", "pgx", "lib/pq":
		attrs = append(attrs, semconv.DBSystemNamePostgreSQL)
	case "sqlite3", "sqlite":
		attrs = append(attrs, semconv.DBSystemNameSQLite)
	case "clickhouse":
		attrs = append(attrs, semconv.DBSystemNameClickHouse)
	case "godror", "oracle", "oci8", "go-oci8":
		attrs = append(attrs, semconv.DBSystemNameOracleDB)
	case "mssql", "sqlserver":
		attrs = append(attrs, semconv.DBSystemNameMicrosoftSQLServer)
	default:
		attrs = append(attrs, semconv.DBSystemNameOtherSQL)
	}

	return attrs
}
