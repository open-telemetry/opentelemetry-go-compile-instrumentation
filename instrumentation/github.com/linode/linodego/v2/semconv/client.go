// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

const (
	// DefaultServerAddress is the default Linode API host used when the
	// instrumented client host is not available to the hook.
	DefaultServerAddress = "api.linode.com"

	// MetricOperationDuration is the latency of public *Client API methods.
	// Per-request HTTP duration is left to net/http instrumentation / spans
	// to avoid duplicating series.
	MetricOperationDuration = "linodego.client.operation.duration"
)

// LinodegoRequest describes a single linodego HTTP request (doRequest) for tracing.
// Public API identity is attached on operation spans via LinodegoOperationTraceAttrs,
// not on request spans.
type LinodegoRequest struct {
	Method   string
	Endpoint string
	// ServerAddress is optional; defaults to DefaultServerAddress when empty.
	ServerAddress string
}

// SpanName returns the span name for a linodego HTTP request (doRequest).
func SpanName(method, endpoint string) string {
	method = strings.ToUpper(strings.TrimSpace(method))
	endpoint = strings.TrimSpace(endpoint)
	if method == "" {
		method = "HTTP"
	}
	if endpoint == "" {
		return method
	}
	return method + " " + endpoint
}

// OperationSpanName returns the span name for a public Client API method.
func OperationSpanName(operation string) string {
	operation = strings.TrimSpace(operation)
	if operation == "" {
		return "linodego.operation"
	}
	return "linodego." + operation
}

// LinodegoRequestTraceAttrs returns trace attributes for a linodego HTTP request.
func LinodegoRequestTraceAttrs(req LinodegoRequest) []attribute.KeyValue {
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = "HTTP"
	}
	endpoint := strings.TrimSpace(req.Endpoint)
	server := strings.TrimSpace(req.ServerAddress)
	if server == "" {
		server = DefaultServerAddress
	}

	attrs := make([]attribute.KeyValue, 0, 3)
	attrs = append(attrs, semconv.HTTPRequestMethodKey.String(method))
	if endpoint != "" {
		path := endpoint
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		attrs = append(attrs, semconv.URLPath(path))
	}
	attrs = append(attrs, semconv.ServerAddress(server))
	return attrs
}

// LinodegoOperationTraceAttrs returns attributes for a public API method span.
func LinodegoOperationTraceAttrs(operation string) []attribute.KeyValue {
	server := DefaultServerAddress
	attrs := []attribute.KeyValue{
		semconv.ServerAddress(server),
	}
	if op := strings.TrimSpace(operation); op != "" {
		attrs = append(attrs, semconv.CodeFunctionName(op))
	}
	return attrs
}

// StatusCodeFromError extracts an HTTP status code from an error when available.
// Supports linodego.Error and any error implementing StatusCode() int,
// including when that error is wrapped (e.g. fmt.Errorf("context: %w", err)).
func StatusCodeFromError(err error) (int, bool) {
	if err == nil {
		return 0, false
	}
	type statusCoder interface {
		StatusCode() int
	}
	var sc statusCoder
	if errors.As(err, &sc) {
		code := sc.StatusCode()
		// linodego uses synthetic codes for non-HTTP errors; only treat
		// real HTTP status codes as response codes.
		if code >= 100 && code < 600 {
			return code, true
		}
	}
	return 0, false
}

// LinodegoErrorTraceAttrs returns attributes derived from a request error.
func LinodegoErrorTraceAttrs(err error) []attribute.KeyValue {
	if err == nil {
		return nil
	}
	attrs := make([]attribute.KeyValue, 0, 2)
	if code, ok := StatusCodeFromError(err); ok {
		attrs = append(attrs, semconv.HTTPResponseStatusCode(code))
		if code >= 400 {
			attrs = append(attrs, semconv.ErrorTypeKey.String(fmt.Sprintf("%d", code)))
		}
	}
	return attrs
}

// HTTPClientStatus returns the span status for an HTTP response status code.
// Matches HTTP client semantic conventions: 4xx/5xx are Error; others Unset.
func HTTPClientStatus(code int) (codes.Code, string) {
	if code < 100 || code >= 600 {
		return codes.Error, fmt.Sprintf("Invalid HTTP status code %d", code)
	}
	if code >= 400 {
		return codes.Error, ""
	}
	return codes.Unset, ""
}

// Metrics holds linodego client instruments.
type Metrics struct {
	operationDuration metric.Float64Histogram
}

// NewMetrics creates linodego client metrics. A nil meter yields no-op metrics.
func NewMetrics(meter metric.Meter) Metrics {
	m := Metrics{}
	if meter == nil {
		return m
	}

	var err error
	m.operationDuration, err = meter.Float64Histogram(
		MetricOperationDuration,
		metric.WithDescription("Duration of linodego public Client API operations."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10),
	)
	if err != nil {
		m.operationDuration = nil
	}

	return m
}

// MetricAttributes returns moderate-cardinality attributes for linodego metrics.
//
// Included (bounded sets):
//   - server.address — fixed default host
//   - code.function.name — public Client method (finite API surface, ~hundreds)
//   - http.response.status_code — when known (HTTP status range)
//
// Omitted (unbounded / path IDs):
//   - url.path / raw endpoints like "linode/instances/123"
//   - resource UIDs, request IDs, etc.
//
// Per-request HTTP method/path detail stays on spans (and optional net/http metrics).
func MetricAttributes(operation string, statusCode int) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 3)
	attrs = append(attrs, semconv.ServerAddress(DefaultServerAddress))
	if op := strings.TrimSpace(operation); op != "" {
		attrs = append(attrs, semconv.CodeFunctionName(op))
	}
	if statusCode > 0 {
		attrs = append(attrs, semconv.HTTPResponseStatusCode(statusCode))
	}
	return attrs
}

// RecordOperationDuration records public API method duration in seconds.
func (m Metrics) RecordOperationDuration(ctx context.Context, seconds float64, operation string, statusCode int) {
	if m.operationDuration == nil {
		return
	}
	m.operationDuration.Record(ctx, seconds, metric.WithAttributes(MetricAttributes(operation, statusCode)...))
}
