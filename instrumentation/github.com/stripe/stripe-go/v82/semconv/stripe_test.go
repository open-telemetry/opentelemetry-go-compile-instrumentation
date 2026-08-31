// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"errors"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

func attrMap(attrs []attribute.KeyValue) map[string]any {
	m := make(map[string]any, len(attrs))
	for _, a := range attrs {
		m[string(a.Key)] = a.Value.AsInterface()
	}
	return m
}

func TestTemplatePath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		// Resource names must survive: they are what makes the span name useful.
		{"collection", "/v1/charges", "/v1/charges"},
		{"underscored resource", "/v1/payment_intents", "/v1/payment_intents"},
		{"v1 namespace", "/v1/billing_portal/sessions", "/v1/billing_portal/sessions"},
		{"v1 namespace with id", "/v1/checkout/sessions/cs_test_a1B2c3", "/v1/checkout/sessions/{id}"},
		{"underscored subresource", "/v1/customers/cus_NffrFeUfNV2Hib/payment_methods",
			"/v1/customers/{id}/payment_methods"},

		// Stripe-generated identifiers must be templated away.
		{"single id", "/v1/customers/cus_NffrFeUfNV2Hib", "/v1/customers/{id}"},
		{"nested ids", "/v1/customers/cus_NffrFeUfNV2Hib/sources/card_1MvoiJ2eZvKYlo2C",
			"/v1/customers/{id}/sources/{id}"},
		{"numeric id", "/v1/invoices/12345", "/v1/invoices/{id}"},
		{"v2 namespaced", "/v2/core/events/evt_1MvoiJ2eZvKYlo2C", "/v2/core/events/{id}"},
		{"already templated", "/v1/customers/{id}", "/v1/customers/{id}"},

		// The version and the segment right after it are never identifiers.
		{"version preserved", "/v1", "/v1"},
		{"query stripped", "/v1/charges?limit=3", "/v1/charges"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, TemplatePath(tt.path))
		})
	}
}

func TestSpanName(t *testing.T) {
	assert.Equal(t, "POST /v1/customers/{id}", SpanName("post", "/v1/customers/cus_NffrFeUfNV2Hib"))
	assert.Equal(t, "GET /v1/charges", SpanName("GET", "/v1/charges"))
	assert.Equal(t, "HTTP", SpanName("", ""))
}

func TestRequestTraceAttrs(t *testing.T) {
	attrs := attrMap(RequestTraceAttrs(Request{
		Method:        "get",
		Path:          "/v1/customers/cus_NffrFeUfNV2Hib",
		ServerAddress: "api.stripe.com",
	}))

	assert.Equal(t, "GET", attrs["http.request.method"])
	assert.Equal(t, "/v1/customers/{id}", attrs["url.template"])
	// The raw path stays on the span so the specific resource is recoverable.
	assert.Equal(t, "/v1/customers/cus_NffrFeUfNV2Hib", attrs["url.path"])
	assert.Equal(t, "api.stripe.com", attrs["server.address"])
}

func TestRequestTraceAttrsDefaultsServerAddress(t *testing.T) {
	attrs := attrMap(RequestTraceAttrs(Request{Method: "GET", Path: "/v1/charges"}))
	assert.Equal(t, DefaultServerAddress, attrs["server.address"])
}

func TestResponseTraceAttrs(t *testing.T) {
	attrs := attrMap(ResponseTraceAttrs(200, "req_abc123"))
	assert.Equal(t, int64(200), attrs["http.response.status_code"])
	assert.Equal(t, "req_abc123", attrs["stripe.request_id"])

	assert.Empty(t, ResponseTraceAttrs(0, ""))
}

func TestErrorTraceAttrs(t *testing.T) {
	t.Run("stripe answered", func(t *testing.T) {
		attrs := attrMap(ErrorTraceAttrs(errors.New("declined"), APIError{
			StatusCode: 402,
			Code:       "card_declined",
			Type:       "card_error",
			RequestID:  "req_abc123",
		}))
		assert.Equal(t, int64(402), attrs["http.response.status_code"])
		assert.Equal(t, "402", attrs["error.type"])
		assert.Equal(t, "card_declined", attrs["stripe.error.code"])
		assert.Equal(t, "card_error", attrs["stripe.error.type"])
		assert.Equal(t, "req_abc123", attrs["stripe.request_id"])
	})

	t.Run("no status code falls back to stripe error type", func(t *testing.T) {
		attrs := attrMap(ErrorTraceAttrs(errors.New("bad"), APIError{Type: "invalid_request_error"}))
		assert.Equal(t, "invalid_request_error", attrs["error.type"])
		assert.NotContains(t, attrs, "http.response.status_code")
	})

	// error.type is required whenever a request ends in error, so a transport
	// failure that never reached Stripe must still carry one.
	t.Run("transport failure still sets error.type", func(t *testing.T) {
		attrs := attrMap(ErrorTraceAttrs(&url.Error{Op: "Post", Err: errors.New("refused")}, APIError{}))
		assert.Equal(t, "*url.Error", attrs["error.type"])
		assert.NotContains(t, attrs, "http.response.status_code")
	})

	t.Run("no error yields nothing", func(t *testing.T) {
		assert.Empty(t, ErrorTraceAttrs(nil, APIError{}))
	})
}

func TestErrorTypePrecedence(t *testing.T) {
	err := errors.New("boom")
	// Status code wins over the Stripe type, which wins over the Go type.
	assert.Equal(t, "404", ErrorType(err, APIError{StatusCode: 404, Type: "invalid_request_error"}))
	assert.Equal(t, "invalid_request_error", ErrorType(err, APIError{Type: "invalid_request_error"}))
	assert.Equal(t, "*errors.errorString", ErrorType(err, APIError{}))
	assert.Equal(t, "_OTHER", ErrorType(nil, APIError{}))
}

func TestServerPortAttrs(t *testing.T) {
	req := Request{Method: "GET", Path: "/v1/charges", ServerAddress: "api.stripe.com", ServerPort: 443}
	assert.Equal(t, int64(443), attrMap(RequestTraceAttrs(req))["server.port"])
	assert.Equal(t, int64(443), attrMap(MetricAttributes(req, 200))["server.port"])

	// Unknown port is omitted rather than reported as zero.
	req.ServerPort = 0
	assert.NotContains(t, attrMap(RequestTraceAttrs(req)), "server.port")
}

func TestHTTPClientStatus(t *testing.T) {
	tests := []struct {
		code int
		want codes.Code
	}{
		{200, codes.Unset},
		{302, codes.Unset},
		{400, codes.Error},
		{402, codes.Error},
		{500, codes.Error},
		{999, codes.Error},
	}
	for _, tt := range tests {
		got, _ := HTTPClientStatus(tt.code)
		assert.Equal(t, tt.want, got, "status %d", tt.code)
	}
}

// Metric labels must stay bounded: the template, never the real path.
func TestMetricAttributesAreBounded(t *testing.T) {
	attrs := attrMap(MetricAttributes(Request{
		Method: "GET",
		Path:   "/v1/customers/cus_NffrFeUfNV2Hib",
	}, 200))

	require.Equal(t, "/v1/customers/{id}", attrs["url.template"])
	assert.NotContains(t, attrs, "url.path")
	assert.NotContains(t, attrs, "stripe.request_id")
	assert.Equal(t, "GET", attrs["http.request.method"])
	assert.Equal(t, int64(200), attrs["http.response.status_code"])
}
