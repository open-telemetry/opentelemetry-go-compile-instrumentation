// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package semconv provides HTTP semantic convention utilities adapted from
// go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp/internal/semconv
package semconv

import (
	"net"
	"net/http"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	upstream "go.opentelemetry.io/otel/semconv/v1.37.0"
)

// SplitHostPort splits a network address hostport of the form "host",
// "host%zone", "[host]", "[host%zone], "host:port", "host%zone:port",
// "[host]:port", "[host%zone]:port", or ":port" into host or host%zone and
// port.
//
// An empty host is returned if it is not provided or unparsable. A negative
// port is returned if it is not provided or unparsable.
func SplitHostPort(hostport string) (host string, port int) {
	port = -1

	if strings.HasPrefix(hostport, "[") {
		addrEnd := strings.LastIndexByte(hostport, ']')
		if addrEnd < 0 {
			// Invalid hostport.
			return host, port
		}
		if i := strings.LastIndexByte(hostport[addrEnd:], ':'); i < 0 {
			host = hostport[1:addrEnd]
			return host, port
		}
	} else {
		if i := strings.LastIndexByte(hostport, ':'); i < 0 {
			host = hostport
			return host, port
		}
	}

	host, pStr, err := net.SplitHostPort(hostport)
	if err != nil {
		return host, port
	}

	p, err := strconv.ParseUint(pStr, 10, 16)
	if err != nil {
		return host, port
	}
	return host, int(p) //nolint:gosec  // Byte size checked 16 above.
}

// RequiredHTTPPort returns the port if it's non-standard for the protocol,
// otherwise returns -1 to indicate it should be omitted.
func RequiredHTTPPort(https bool, port int) int {
	if https {
		if port > 0 && port != 443 {
			return port
		}
	} else {
		if port > 0 && port != 80 {
			return port
		}
	}
	return -1
}

// ServerClientIP extracts the client IP from X-Forwarded-For header.
func ServerClientIP(xForwardedFor string) string {
	if idx := strings.IndexByte(xForwardedFor, ','); idx >= 0 {
		xForwardedFor = xForwardedFor[:idx]
	}
	return xForwardedFor
}

// HTTPRoute extracts the route from a pattern string (e.g., "GET /api/users").
func HTTPRoute(pattern string) string {
	if idx := strings.IndexByte(pattern, '/'); idx >= 0 {
		return pattern[idx:]
	}
	return ""
}

// NetProtocol parses protocol name and version from a protocol string like "HTTP/1.1".
func NetProtocol(proto string) (name, version string) {
	name, version, _ = strings.Cut(proto, "/")
	switch name {
	case "HTTP":
		name = "http"
	case "QUIC":
		name = "quic"
	case "SPDY":
		name = "spdy"
	default:
		name = strings.ToLower(name)
	}
	return name, version
}

// MethodLookup maps HTTP methods to their semconv attribute values.
var MethodLookup = map[string]attribute.KeyValue{
	http.MethodConnect: upstream.HTTPRequestMethodConnect,
	http.MethodDelete:  upstream.HTTPRequestMethodDelete,
	http.MethodGet:     upstream.HTTPRequestMethodGet,
	http.MethodHead:    upstream.HTTPRequestMethodHead,
	http.MethodOptions: upstream.HTTPRequestMethodOptions,
	http.MethodPatch:   upstream.HTTPRequestMethodPatch,
	http.MethodPost:    upstream.HTTPRequestMethodPost,
	http.MethodPut:     upstream.HTTPRequestMethodPut,
	http.MethodTrace:   upstream.HTTPRequestMethodTrace,
	"QUERY":            upstream.HTTPRequestMethodKey.String("QUERY"),
}

// HandleErr reports errors to the OTel error handler.
func HandleErr(err error) {
	if err != nil {
		otel.Handle(err)
	}
}

// StandardizeHTTPMethod normalizes HTTP method strings.
// Returns "_OTHER" for non-standard methods.
func StandardizeHTTPMethod(method string) string {
	method = strings.ToUpper(method)
	switch method {
	case http.MethodConnect, http.MethodDelete, http.MethodGet, http.MethodHead,
		http.MethodOptions, http.MethodPatch, http.MethodPost, http.MethodPut, http.MethodTrace, "QUERY":
	default:
		method = "_OTHER"
	}
	return method
}
