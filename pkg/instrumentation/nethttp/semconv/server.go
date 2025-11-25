// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

// RequestTraceAttrsOpts provides options for request trace attributes.
type RequestTraceAttrsOpts struct {
	// If set, this is used as value for the "client.address" attribute.
	HTTPClientIP string
}

// ResponseTelemetry holds response telemetry data.
type ResponseTelemetry struct {
	StatusCode int
	ReadBytes  int64
	ReadError  error
	WriteBytes int64
	WriteError error
}

// HTTPServer provides HTTP semantic convention attributes and metrics for server requests.
type HTTPServer struct {
	requestBodySize  metric.Int64Histogram
	responseBodySize metric.Int64Histogram
	requestDuration  metric.Float64Histogram
	activeRequests   metric.Int64UpDownCounter
}

// NewHTTPServer creates a new HTTPServer instance with metrics.
// If meter is nil, returns a server without metrics support.
func NewHTTPServer(meter metric.Meter) HTTPServer {
	server := HTTPServer{}

	if meter == nil {
		return server
	}

	var err error
	server.requestBodySize, err = meter.Int64Histogram(
		"http.server.request.body.size",
		metric.WithDescription("Size of HTTP server request bodies."),
		metric.WithUnit("By"),
	)
	HandleErr(err)

	server.responseBodySize, err = meter.Int64Histogram(
		"http.server.response.body.size",
		metric.WithDescription("Size of HTTP server response bodies."),
		metric.WithUnit("By"),
	)
	HandleErr(err)

	server.requestDuration, err = meter.Float64Histogram(
		"http.server.request.duration",
		metric.WithDescription("Duration of HTTP server requests."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10),
	)
	HandleErr(err)

	server.activeRequests, err = meter.Int64UpDownCounter(
		"http.server.active_requests",
		metric.WithDescription("Number of active HTTP server requests."),
		metric.WithUnit("{request}"),
	)
	HandleErr(err)

	return server
}

// Status returns a span status code and message for an HTTP status code
// value returned by a server. Status codes in the 400-499 range are not
// returned as errors (per HTTP semconv spec).
func (HTTPServer) Status(code int) (codes.Code, string) {
	if code < 100 || code >= 600 {
		return codes.Error, fmt.Sprintf("Invalid HTTP status code %d", code)
	}
	if code >= 500 {
		return codes.Error, ""
	}
	return codes.Unset, ""
}

// RequestTraceAttrs returns trace attributes for an HTTP request received by a server.
// The server parameter should be the primary server name if known.
func (n HTTPServer) RequestTraceAttrs(
	server string,
	req *http.Request,
	opts RequestTraceAttrsOpts,
) []attribute.KeyValue {
	count := 3 // ServerAddress, Method, Scheme

	var host string
	var p int
	if server == "" {
		host, p = SplitHostPort(req.Host)
	} else {
		host, p = SplitHostPort(server)
		if p < 0 {
			_, p = SplitHostPort(req.Host)
		}
	}

	hostPort := RequiredHTTPPort(req.TLS != nil, p)
	if hostPort > 0 {
		count++
	}

	method, methodOriginal := n.method(req.Method)
	if methodOriginal != (attribute.KeyValue{}) {
		count++
	}

	scheme := n.scheme(req.TLS != nil)

	peer, peerPort := SplitHostPort(req.RemoteAddr)
	if peer != "" {
		count++
		if peerPort > 0 {
			count++
		}
	}

	useragent := req.UserAgent()
	if useragent != "" {
		count++
	}

	// For client IP, use, in order:
	// 1. The value passed in the options
	// 2. The value in the X-Forwarded-For header
	// 3. The peer address
	clientIP := opts.HTTPClientIP
	if clientIP == "" {
		clientIP = ServerClientIP(req.Header.Get("X-Forwarded-For"))
		if clientIP == "" {
			clientIP = peer
		}
	}
	if clientIP != "" {
		count++
	}

	if req.URL != nil && req.URL.Path != "" {
		count++
	}

	if req.URL != nil && req.URL.RawQuery != "" {
		count++
	}

	protoName, protoVersion := NetProtocol(req.Proto)
	if protoName != "" && protoName != "http" {
		count++
	}
	if protoVersion != "" {
		count++
	}

	// Use r.Pattern for HTTP route detection (Go 1.22+)
	route := HTTPRoute(req.Pattern)
	if route != "" {
		count++
	}

	attrs := make([]attribute.KeyValue, 0, count)
	attrs = append(attrs,
		semconv.ServerAddress(host),
		method,
		scheme,
	)

	if hostPort > 0 {
		attrs = append(attrs, semconv.ServerPort(hostPort))
	}
	if methodOriginal != (attribute.KeyValue{}) {
		attrs = append(attrs, methodOriginal)
	}

	if peer != "" {
		attrs = append(attrs, semconv.NetworkPeerAddress(peer))
		if peerPort > 0 {
			attrs = append(attrs, semconv.NetworkPeerPort(peerPort))
		}
	}

	if useragent != "" {
		attrs = append(attrs, semconv.UserAgentOriginal(useragent))
	}

	if clientIP != "" {
		attrs = append(attrs, semconv.ClientAddress(clientIP))
	}

	if req.URL != nil && req.URL.Path != "" {
		attrs = append(attrs, semconv.URLPath(req.URL.Path))
	}

	if req.URL != nil && req.URL.RawQuery != "" {
		attrs = append(attrs, semconv.URLQuery(req.URL.RawQuery))
	}

	if protoName != "" && protoName != "http" {
		attrs = append(attrs, semconv.NetworkProtocolName(protoName))
	}
	if protoVersion != "" {
		attrs = append(attrs, semconv.NetworkProtocolVersion(protoVersion))
	}

	if route != "" {
		attrs = append(attrs, n.Route(route))
	}

	return attrs
}

// ResponseTraceAttrs returns trace attributes for telemetry from an HTTP response.
func (HTTPServer) ResponseTraceAttrs(resp ResponseTelemetry) []attribute.KeyValue {
	var count int

	if resp.ReadBytes > 0 {
		count++
	}
	if resp.WriteBytes > 0 {
		count++
	}
	if resp.StatusCode > 0 {
		count++
	}

	// Add error.type for 5xx status codes
	if resp.StatusCode >= 500 && resp.StatusCode < 600 {
		count++
	}

	attributes := make([]attribute.KeyValue, 0, count)

	if resp.ReadBytes > 0 {
		attributes = append(attributes,
			semconv.HTTPRequestBodySize(int(resp.ReadBytes)),
		)
	}
	if resp.WriteBytes > 0 {
		attributes = append(attributes,
			semconv.HTTPResponseBodySize(int(resp.WriteBytes)),
		)
	}
	if resp.StatusCode > 0 {
		attributes = append(attributes,
			semconv.HTTPResponseStatusCode(resp.StatusCode),
		)
	}

	// Add error.type for 5xx status codes
	if resp.StatusCode >= 500 && resp.StatusCode < 600 {
		attributes = append(attributes,
			semconv.ErrorTypeKey.String(strconv.Itoa(resp.StatusCode)),
		)
	}

	return attributes
}

// Route returns the attribute for the HTTP route.
func (HTTPServer) Route(route string) attribute.KeyValue {
	return semconv.HTTPRoute(route)
}

// NetworkTransportAttr returns the network.transport attribute based on network type.
func (HTTPServer) NetworkTransportAttr(network string) []attribute.KeyValue {
	attr := semconv.NetworkTransportPipe
	switch network {
	case "tcp", "tcp4", "tcp6":
		attr = semconv.NetworkTransportTCP
	case "udp", "udp4", "udp6":
		attr = semconv.NetworkTransportUDP
	case "unix", "unixgram", "unixpacket":
		attr = semconv.NetworkTransportUnix
	}

	return []attribute.KeyValue{attr}
}

// MetricAttributes returns attributes for HTTP server metrics.
func (n HTTPServer) MetricAttributes(
	server string,
	req *http.Request,
	statusCode int,
	route string,
	additionalAttributes []attribute.KeyValue,
) []attribute.KeyValue {
	num := len(additionalAttributes) + 3
	var host string
	var p int
	if server == "" {
		host, p = SplitHostPort(req.Host)
	} else {
		host, p = SplitHostPort(server)
		if p < 0 {
			_, p = SplitHostPort(req.Host)
		}
	}
	hostPort := RequiredHTTPPort(req.TLS != nil, p)
	if hostPort > 0 {
		num++
	}
	protoName, protoVersion := NetProtocol(req.Proto)
	if protoName != "" {
		num++
	}
	if protoVersion != "" {
		num++
	}

	if statusCode > 0 {
		num++
	}

	if route != "" {
		num++
	}

	attributes := make([]attribute.KeyValue, 0, num)
	attributes = append(attributes, additionalAttributes...)
	attributes = append(attributes,
		semconv.HTTPRequestMethodKey.String(StandardizeHTTPMethod(req.Method)),
		n.scheme(req.TLS != nil),
		semconv.ServerAddress(host))

	if hostPort > 0 {
		attributes = append(attributes, semconv.ServerPort(hostPort))
	}
	if protoName != "" {
		attributes = append(attributes, semconv.NetworkProtocolName(protoName))
	}
	if protoVersion != "" {
		attributes = append(attributes, semconv.NetworkProtocolVersion(protoVersion))
	}

	if statusCode > 0 {
		attributes = append(attributes, semconv.HTTPResponseStatusCode(statusCode))
	}

	if route != "" {
		attributes = append(attributes, semconv.HTTPRoute(route))
	}
	return attributes
}

// method returns the HTTP method attribute and optional original method attribute.
func (HTTPServer) method(method string) (attribute.KeyValue, attribute.KeyValue) {
	if method == "" {
		return semconv.HTTPRequestMethodGet, attribute.KeyValue{}
	}
	if attr, ok := MethodLookup[method]; ok {
		return attr, attribute.KeyValue{}
	}

	orig := semconv.HTTPRequestMethodOriginal(method)
	if attr, ok := MethodLookup[strings.ToUpper(method)]; ok {
		return attr, orig
	}
	return semconv.HTTPRequestMethodGet, orig
}

// scheme returns the URL scheme attribute.
func (HTTPServer) scheme(https bool) attribute.KeyValue {
	if https {
		return semconv.URLScheme("https")
	}
	return semconv.URLScheme("http")
}

// RecordMetrics records HTTP server metrics.
func (n HTTPServer) RecordMetrics(
	ctx context.Context,
	server string,
	req *http.Request,
	statusCode int,
	route string,
	requestSize, responseSize int64,
	elapsedTime float64,
	additionalAttributes []attribute.KeyValue,
) {
	if n.requestBodySize == nil && n.responseBodySize == nil && n.requestDuration == nil {
		return
	}

	attributes := n.MetricAttributes(server, req, statusCode, route, additionalAttributes)
	opts := metric.WithAttributeSet(attribute.NewSet(attributes...))

	if n.requestBodySize != nil && requestSize > 0 {
		n.requestBodySize.Record(ctx, requestSize, opts)
	}

	if n.responseBodySize != nil && responseSize > 0 {
		n.responseBodySize.Record(ctx, responseSize, opts)
	}

	if n.requestDuration != nil {
		// elapsedTime should be in seconds
		n.requestDuration.Record(ctx, elapsedTime, opts)
	}
}

// Package-level convenience functions for direct use in hooks.
// These use a server without metrics support.

var defaultHTTPServer = NewHTTPServer(nil)

// HTTPServerRequestTraceAttrs returns trace attributes for an HTTP server request.
func HTTPServerRequestTraceAttrs(server string, req *http.Request) []attribute.KeyValue {
	return defaultHTTPServer.RequestTraceAttrs(server, req, RequestTraceAttrsOpts{})
}

// HTTPServerResponseTraceAttrs returns trace attributes for an HTTP server response.
func HTTPServerResponseTraceAttrs(statusCode int, writeBytes int64) []attribute.KeyValue {
	return defaultHTTPServer.ResponseTraceAttrs(ResponseTelemetry{
		StatusCode: statusCode,
		WriteBytes: writeBytes,
	})
}

// HTTPServerStatus returns span status code based on HTTP response status code.
func HTTPServerStatus(code int) (codes.Code, string) {
	return defaultHTTPServer.Status(code)
}

// HTTPServerRoute returns the HTTP route attribute.
func HTTPServerRoute(route string) attribute.KeyValue {
	return defaultHTTPServer.Route(route)
}

// HTTPServerSpanName returns the span name for an HTTP server request.
func HTTPServerSpanName(method, route string) string {
	if route != "" {
		return method + " " + route
	}
	return method
}
