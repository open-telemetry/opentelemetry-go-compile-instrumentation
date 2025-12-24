// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

// HTTPClient provides HTTP semantic convention attributes and metrics for client requests.
type HTTPClient struct {
	requestBodySize    metric.Int64Histogram
	responseBodySize   metric.Int64Histogram
	requestDuration    metric.Float64Histogram
	activeRequests     metric.Int64UpDownCounter
	openConnections    metric.Int64UpDownCounter
	connectionDuration metric.Float64Histogram
}

// NewHTTPClient creates a new HTTPClient instance with metrics.
// If meter is nil, returns a client without metrics support.
func NewHTTPClient(meter metric.Meter) HTTPClient {
	client := HTTPClient{}

	if meter == nil {
		return client
	}

	var err error
	client.requestBodySize, err = meter.Int64Histogram(
		"http.client.request.body.size",
		metric.WithDescription("Size of HTTP client request bodies."),
		metric.WithUnit("By"),
	)
	HandleErr(err)

	client.responseBodySize, err = meter.Int64Histogram(
		"http.client.response.body.size",
		metric.WithDescription("Size of HTTP client response bodies."),
		metric.WithUnit("By"),
	)
	HandleErr(err)

	client.requestDuration, err = meter.Float64Histogram(
		"http.client.request.duration",
		metric.WithDescription("Duration of HTTP client requests."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10),
	)
	HandleErr(err)

	client.activeRequests, err = meter.Int64UpDownCounter(
		"http.client.active_requests",
		metric.WithDescription("Number of active HTTP requests."),
		metric.WithUnit("{request}"),
	)
	HandleErr(err)

	client.openConnections, err = meter.Int64UpDownCounter(
		"http.client.open_connections",
		metric.WithDescription("Number of outbound HTTP connections that are currently active or idle on the client."),
		metric.WithUnit("{connection}"),
	)
	HandleErr(err)

	client.connectionDuration, err = meter.Float64Histogram(
		"http.client.connection.duration",
		metric.WithDescription("The duration of the successfully established outbound HTTP connections."),
		metric.WithUnit("s"),
	)
	HandleErr(err)

	return client
}

// Status returns the span status code based on HTTP response status code.
func (HTTPClient) Status(code int) (codes.Code, string) {
	if code < 100 || code >= 600 {
		return codes.Error, fmt.Sprintf("Invalid HTTP status code %d", code)
	}
	if code >= 400 {
		return codes.Error, ""
	}
	return codes.Unset, ""
}

// RequestTraceAttrs returns trace attributes for an HTTP request made by a client.
// Returns: http.request.method, http.request.method.original, url.full,
// server.address, server.port, network.protocol.name, network.protocol.version,
// url.scheme, user_agent.original
func (n HTTPClient) RequestTraceAttrs(req *http.Request) []attribute.KeyValue {
	numOfAttributes := 4 // URL, server address, method, and scheme.

	var urlHost string
	if req.URL != nil {
		urlHost = req.URL.Host
	}
	var requestHost string
	var requestPort int
	for _, hostport := range []string{urlHost, req.Header.Get("Host")} {
		requestHost, requestPort = SplitHostPort(hostport)
		if requestHost != "" || requestPort > 0 {
			break
		}
	}

	eligiblePort := RequiredHTTPPort(req.URL != nil && req.URL.Scheme == "https", requestPort)
	if eligiblePort > 0 {
		numOfAttributes++
	}

	protoName, protoVersion := NetProtocol(req.Proto)
	if protoName != "" && protoName != "http" {
		numOfAttributes++
	}
	if protoVersion != "" {
		numOfAttributes++
	}

	method, originalMethod := n.method(req.Method)
	if originalMethod != (attribute.KeyValue{}) {
		numOfAttributes++
	}

	useragent := req.UserAgent()
	if useragent != "" {
		numOfAttributes++
	}

	attrs := make([]attribute.KeyValue, 0, numOfAttributes)

	attrs = append(attrs, method)
	if originalMethod != (attribute.KeyValue{}) {
		attrs = append(attrs, originalMethod)
	}

	var u string
	if req.URL != nil {
		// Remove any username/password info that may be in the URL.
		userinfo := req.URL.User
		req.URL.User = nil
		u = req.URL.String()
		// Restore any username/password info that was removed.
		req.URL.User = userinfo
	}
	attrs = append(attrs, semconv.URLFull(u))

	attrs = append(attrs, semconv.ServerAddress(requestHost))
	if eligiblePort > 0 {
		attrs = append(attrs, semconv.ServerPort(eligiblePort))
	}

	// Add url.scheme
	attrs = append(attrs, n.traceScheme(req))

	if protoName != "" && protoName != "http" {
		attrs = append(attrs, semconv.NetworkProtocolName(protoName))
	}
	if protoVersion != "" {
		attrs = append(attrs, semconv.NetworkProtocolVersion(protoVersion))
	}

	if useragent != "" {
		attrs = append(attrs, semconv.UserAgentOriginal(useragent))
	}

	return attrs
}

// ResponseTraceAttrs returns trace attributes for an HTTP response made by a client.
// Returns: http.response.status_code, error.type
func (HTTPClient) ResponseTraceAttrs(resp *http.Response) []attribute.KeyValue {
	var count int
	if resp.StatusCode > 0 {
		count++
	}

	if isErrorStatusCode(resp.StatusCode) {
		count++
	}

	attrs := make([]attribute.KeyValue, 0, count)
	if resp.StatusCode > 0 {
		attrs = append(attrs, semconv.HTTPResponseStatusCode(resp.StatusCode))
	}

	if isErrorStatusCode(resp.StatusCode) {
		errorType := strconv.Itoa(resp.StatusCode)
		attrs = append(attrs, semconv.ErrorTypeKey.String(errorType))
	}
	return attrs
}

// ErrorType returns the error.type attribute for a given error.
func (HTTPClient) ErrorType(err error) attribute.KeyValue {
	t := reflect.TypeOf(err)
	var value string
	if t.PkgPath() == "" && t.Name() == "" {
		// Likely a builtin type.
		value = t.String()
	} else {
		value = fmt.Sprintf("%s.%s", t.PkgPath(), t.Name())
	}

	if value == "" {
		return semconv.ErrorTypeOther
	}

	return semconv.ErrorTypeKey.String(value)
}

// method returns the HTTP method attribute and optional original method attribute.
func (HTTPClient) method(method string) (attribute.KeyValue, attribute.KeyValue) {
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

// MetricAttributes returns attributes for HTTP client metrics.
func (n HTTPClient) MetricAttributes(
	req *http.Request,
	statusCode int,
	additionalAttributes []attribute.KeyValue,
) []attribute.KeyValue {
	num := len(additionalAttributes) + 3 // method, server.address, url.scheme
	var h string
	if req.URL != nil {
		h = req.URL.Host
	}
	var requestHost string
	var requestPort int
	for _, hostport := range []string{h, req.Header.Get("Host")} {
		requestHost, requestPort = SplitHostPort(hostport)
		if requestHost != "" || requestPort > 0 {
			break
		}
	}

	port := RequiredHTTPPort(req.URL != nil && req.URL.Scheme == "https", requestPort)
	if port > 0 {
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

	attributes := make([]attribute.KeyValue, 0, num)
	attributes = append(attributes, additionalAttributes...)
	attributes = append(attributes,
		semconv.HTTPRequestMethodKey.String(StandardizeHTTPMethod(req.Method)),
		semconv.ServerAddress(requestHost),
		n.scheme(req),
	)

	if port > 0 {
		attributes = append(attributes, semconv.ServerPort(port))
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
	return attributes
}

// scheme returns the URL scheme attribute for metrics.
func (HTTPClient) scheme(req *http.Request) attribute.KeyValue {
	if req.URL != nil && req.URL.Scheme != "" {
		return semconv.URLScheme(req.URL.Scheme)
	}
	if req.TLS != nil {
		return semconv.URLScheme("https")
	}
	return semconv.URLScheme("http")
}

// traceScheme returns the URL scheme attribute for traces.
func (HTTPClient) traceScheme(req *http.Request) attribute.KeyValue {
	if req.URL != nil && req.URL.Scheme != "" {
		return semconv.URLScheme(req.URL.Scheme)
	}
	if req.TLS != nil {
		return semconv.URLScheme("https")
	}
	return semconv.URLScheme("http")
}

// isErrorStatusCode returns true if the HTTP status code indicates an error.
func isErrorStatusCode(code int) bool {
	return code >= 400 || code < 100
}

// RecordMetrics records HTTP client metrics.
func (n HTTPClient) RecordMetrics(
	ctx context.Context,
	req *http.Request,
	statusCode int,
	requestSize int64,
	responseSize int64,
	elapsedTime float64,
	additionalAttributes []attribute.KeyValue,
) {
	if n.requestBodySize == nil && n.responseBodySize == nil && n.requestDuration == nil {
		return
	}

	attributes := n.MetricAttributes(req, statusCode, additionalAttributes)
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
// These use a client without metrics support.

var defaultHTTPClient = NewHTTPClient(nil)

// HTTPClientRequestTraceAttrs returns trace attributes for an HTTP client request.
func HTTPClientRequestTraceAttrs(req *http.Request) []attribute.KeyValue {
	return defaultHTTPClient.RequestTraceAttrs(req)
}

// HTTPClientResponseTraceAttrs returns trace attributes for an HTTP client response.
func HTTPClientResponseTraceAttrs(resp *http.Response) []attribute.KeyValue {
	return defaultHTTPClient.ResponseTraceAttrs(resp)
}

// HTTPClientStatus returns span status code based on HTTP response status code.
func HTTPClientStatus(code int) (codes.Code, string) {
	return defaultHTTPClient.Status(code)
}

// HTTPClientErrorType returns the error.type attribute for a given error.
func HTTPClientErrorType(err error) attribute.KeyValue {
	return defaultHTTPClient.ErrorType(err)
}
