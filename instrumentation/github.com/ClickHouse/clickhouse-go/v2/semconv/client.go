// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package semconv builds the attributes and names of ClickHouse client spans
// following the OpenTelemetry database semantic conventions.
package semconv

import (
	"net"
	"strconv"
	"strings"
	"unicode"

	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

// Server describes the ClickHouse server a conn talks to.
type Server struct {
	Address   string
	Port      int
	Namespace string
}

// ServerFromAddrs returns the server of a conn configured with addrs and
// database. The address is recorded only when the conn has a single address:
// with several, the hook cannot tell which one served the call.
func ServerFromAddrs(addrs []string, database string) Server {
	s := Server{Namespace: database}
	if len(addrs) != 1 {
		return s
	}
	host, port, splitErr := net.SplitHostPort(addrs[0])
	if splitErr != nil {
		s.Address = addrs[0]
		return s
	}
	s.Address = host
	if p, err := strconv.Atoi(port); err == nil && p > 0 {
		s.Port = p
	}
	return s
}

// Request describes one call to ClickHouse.
type Request struct {
	// Operation is db.operation.name, e.g. "SELECT" or "PING".
	Operation string
	// Query is db.query.text; empty for calls without a query.
	Query string
	Server
}

// ClientTraceAttrs returns the attributes of the span of req.
func ClientTraceAttrs(req Request) []attribute.KeyValue {
	// db.system.name and at most five more.
	const maxAttrs = 6
	attrs := make([]attribute.KeyValue, 0, maxAttrs)
	attrs = append(attrs, semconv.DBSystemNameClickHouse)
	if req.Operation != "" {
		attrs = append(attrs, semconv.DBOperationName(req.Operation))
	}
	if req.Query != "" {
		attrs = append(attrs, semconv.DBQueryText(req.Query))
	}
	if req.Namespace != "" {
		attrs = append(attrs, semconv.DBNamespace(req.Namespace))
	}
	if req.Address != "" {
		attrs = append(attrs, semconv.ServerAddress(req.Address))
	}
	if req.Port > 0 {
		attrs = append(attrs, semconv.ServerPort(req.Port))
	}
	return attrs
}

// SpanName returns "{operation} {namespace}", or the part that is known, or
// "clickhouse" when neither is.
func SpanName(operation, namespace string) string {
	switch {
	case operation != "" && namespace != "":
		return operation + " " + namespace
	case operation != "":
		return operation
	case namespace != "":
		return namespace
	default:
		return "clickhouse"
	}
}

// ErrorAttrs returns error.type and db.response.status_code for a ClickHouse
// server exception with code, or error.type "_OTHER" when code is 0, i.e. the
// error did not come from the server.
func ErrorAttrs(code int32) []attribute.KeyValue {
	if code == 0 {
		return []attribute.KeyValue{semconv.ErrorTypeOther}
	}
	c := strconv.Itoa(int(code))
	return []attribute.KeyValue{semconv.ErrorTypeKey.String(c), semconv.DBResponseStatusCode(c)}
}

// OperationName returns the first keyword of query, upper-cased, after
// leading whitespace, parentheses and comments; "" if there is none.
func OperationName(query string) string {
	q := skipPrefix(query)
	end := strings.IndexFunc(q, func(r rune) bool {
		return !unicode.IsLetter(r) && r != '_'
	})
	if end >= 0 {
		q = q[:end]
	}
	return strings.ToUpper(q)
}

func skipPrefix(q string) string {
	for {
		q = strings.TrimLeftFunc(q, func(r rune) bool { return unicode.IsSpace(r) || r == '(' })
		switch {
		case strings.HasPrefix(q, "/*"):
			end := strings.Index(q, "*/")
			if end < 0 {
				return ""
			}
			q = q[end+2:]
		case strings.HasPrefix(q, "--"), strings.HasPrefix(q, "#"):
			end := strings.IndexAny(q, "\r\n")
			if end < 0 {
				return ""
			}
			q = q[end:]
		default:
			return q
		}
	}
}
