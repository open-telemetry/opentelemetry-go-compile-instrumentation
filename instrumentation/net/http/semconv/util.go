// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package semconv provides HTTP semantic convention utilities adapted from
// go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp/internal/semconv
package semconv

import (
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

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

// MethodLookup maps the default known HTTP methods to their semconv attribute
// values. OTEL_INSTRUMENTATION_HTTP_KNOWN_METHODS may fully replace this set.
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

// HTTPKnownMethodsEnv is the environment variable that fully overrides the
// default known HTTP methods (comma-separated, case-sensitive).
const HTTPKnownMethodsEnv = "OTEL_INSTRUMENTATION_HTTP_KNOWN_METHODS"

// knownMethods is the active known-method set: either MethodLookup, or a full
// replacement parsed from HTTPKnownMethodsEnv on first use.
var knownMethods = sync.OnceValue(func() map[string]attribute.KeyValue {
	if env := os.Getenv(HTTPKnownMethodsEnv); env != "" {
		return parseKnownMethods(env)
	}
	return MethodLookup
})

// parseKnownMethods parses a comma-separated, case-sensitive override list.
// Whitespace around each method is trimmed; empty entries are skipped.
func parseKnownMethods(env string) map[string]attribute.KeyValue {
	parts := strings.Split(env, ",")
	out := make(map[string]attribute.KeyValue, len(parts))
	for _, part := range parts {
		method := strings.TrimSpace(part)
		if method == "" {
			continue
		}
		if attr, ok := MethodLookup[method]; ok {
			out[method] = attr
			continue
		}
		out[method] = upstream.HTTPRequestMethodKey.String(method)
	}
	return out
}

// HandleErr reports errors to the OTel error handler.
func HandleErr(err error) {
	if err != nil {
		otel.Handle(err)
	}
}

// StandardizeHTTPMethod normalizes HTTP method strings for metrics.
// Returns "_OTHER" for methods not in the known-method set.
func StandardizeHTTPMethod(method string) string {
	lookup := knownMethods()
	if _, ok := lookup[method]; ok {
		return method
	}
	upper := strings.ToUpper(method)
	if _, ok := lookup[upper]; ok {
		return upper
	}
	return "_OTHER"
}

// requestMethodAttrs returns http.request.method and, when needed,
// http.request.method_original. Unknown and empty methods use _OTHER.
// method_original is omitted when it would equal http.request.method.
func requestMethodAttrs(method string) (attribute.KeyValue, attribute.KeyValue) {
	if method == "" {
		return upstream.HTTPRequestMethodOther, attribute.KeyValue{}
	}

	lookup := knownMethods()
	if attr, ok := lookup[method]; ok {
		return attr, attribute.KeyValue{}
	}

	if attr, ok := lookup[strings.ToUpper(method)]; ok {
		return attr, upstream.HTTPRequestMethodOriginal(method)
	}

	// Literal "_OTHER" is already the fallback value — don't set method_original.
	if method == "_OTHER" {
		return upstream.HTTPRequestMethodOther, attribute.KeyValue{}
	}
	return upstream.HTTPRequestMethodOther, upstream.HTTPRequestMethodOriginal(method)
}

// SpanMethod returns the method token used in HTTP span names.
// Unknown methods become "HTTP" per the semantic conventions.
func SpanMethod(method string) string {
	standardized := StandardizeHTTPMethod(method)
	if standardized == "_OTHER" {
		return "HTTP"
	}
	return standardized
}
