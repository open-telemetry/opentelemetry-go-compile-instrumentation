// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"net/url"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

// Request describes a single Elasticsearch REST call for building trace attributes.
type Request struct {
	Method    string
	Path      string
	Operation string
	Index     string
}

// SpanName returns the client span name: "{operation} {index}", or one of
// those parts, or "elasticsearch" when both are empty.
func SpanName(operation, index string) string {
	operation = strings.TrimSpace(operation)
	index = strings.TrimSpace(index)
	switch {
	case operation != "" && index != "":
		return operation + " " + index
	case operation != "":
		return operation
	case index != "":
		return index
	default:
		return "elasticsearch"
	}
}

// ClientTraceAttrs returns trace attributes for an Elasticsearch client request.
func ClientTraceAttrs(req Request) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		semconv.DBSystemNameElasticsearch,
		semconv.NetworkTransportTCP,
	}
	if op := strings.TrimSpace(req.Operation); op != "" {
		attrs = append(attrs, semconv.DBOperationName(op))
	}
	if index := strings.TrimSpace(req.Index); index != "" {
		attrs = append(attrs, semconv.DBCollectionName(index))
	}
	if method := strings.ToUpper(strings.TrimSpace(req.Method)); method != "" {
		attrs = append(attrs, semconv.HTTPRequestMethodKey.String(method))
	}
	if path := normalizePath(req.Path); path != "" {
		attrs = append(attrs, semconv.URLPath(path))
	}
	return attrs
}

func normalizePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if u, err := url.PathUnescape(path); err == nil {
		path = u
	}
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	if path != "" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}
