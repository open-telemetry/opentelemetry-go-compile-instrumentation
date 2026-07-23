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
	"go.opentelemetry.io/otel/semconv/v1.37.0/httpconv"
)

// HTTPClient provides HTTP semantic convention attributes and metrics for client requests.
type HTTPClient struct {
	requestBodySize    httpconv.ClientRequestBodySize
	responseBodySize   httpconv.ClientResponseBodySize
	requestDuration    httpconv.ClientRequestDuration
	activeRequests     httpconv.ClientActiveRequests
	openConnections    httpconv.ClientOpenConnections
	connectionDuration httpconv.ClientConnectionDuration
}

// NewHTTPClient creates a new HTTPClient instance with metrics.
// If meter is nil, the client uses no-op metrics.
func NewHTTPClient(meter metric.Meter) HTTPClient {
	var err error
	client := HTTPClient{}
	client.requestBodySize, err = httpconv.NewClientRequestBodySize(meter)
	HandleErr(err)

	client.responseBodySize, err = httpconv.NewClientResponseBodySize(meter)
	HandleErr(err)

	client.requestDuration, err = httpconv.NewClientRequestDuration(
		meter,
		metric.WithExplicitBucketBoundaries(requestDurationBucketBoundaries...),
	)
	HandleErr(err)

	client.activeRequests, err = httpconv.NewClientActiveRequests(meter)
	HandleErr(err)

	client.openConnections, err = httpconv.NewClientOpenConnections(meter)
	HandleErr(err)

	client.connectionDuration, err = httpconv.NewClientConnectionDuration(meter)
	HandleErr(err)

	return client
}

func (n HTTPClient) metricNames() []string {
	return []string{
		n.requestBodySize.Name(),
		n.responseBodySize.Name(),
		n.requestDuration.Name(),
		n.activeRequests.Name(),
		n.openConnections.Name(),
		n.connectionDuration.Name(),
	}
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
	return n.metricAttributes(req, statusCode, req.Proto, additionalAttributes)
}

func (n HTTPClient) metricAttributes(
	req *http.Request,
	statusCode int,
	networkProtocol string,
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

	if networkProtocol == "" {
		networkProtocol = req.Proto
	}
	protoName, protoVersion := NetProtocol(networkProtocol)
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

// ActiveRequestMetricAttributes returns attributes for the active request metric.
func (n HTTPClient) ActiveRequestMetricAttributes(
	req *http.Request,
	additionalAttributes []attribute.KeyValue,
) []attribute.KeyValue {
	attributes := n.MetricAttributes(req, 0, additionalAttributes)
	result := attributes[:0]
	for _, attr := range attributes {
		if attr.Key == semconv.NetworkProtocolNameKey || attr.Key == semconv.NetworkProtocolVersionKey {
			continue
		}
		result = append(result, attr)
	}
	return result
}

// AddActiveRequests adds incr to the number of active HTTP client requests.
func (n HTTPClient) AddActiveRequests(ctx context.Context, incr int64, set attribute.Set) {
	n.activeRequests.AddSet(ctx, incr, set)
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
	networkProtocol string,
	requestSize int64,
	responseSize int64,
	elapsedTime float64,
	additionalAttributes []attribute.KeyValue,
) {
	attributes := n.metricAttributes(req, statusCode, networkProtocol, additionalAttributes)
	set := attribute.NewSet(attributes...)

	if requestSize > 0 {
		n.requestBodySize.RecordSet(ctx, requestSize, set)
	}

	if responseSize > 0 {
		n.responseBodySize.RecordSet(ctx, responseSize, set)
	}

	// elapsedTime should be in seconds
	n.requestDuration.RecordSet(ctx, elapsedTime, set)
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
