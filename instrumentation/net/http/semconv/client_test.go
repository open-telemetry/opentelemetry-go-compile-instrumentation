// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric/noop"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

func TestHTTPClientRequestTraceAttrs(t *testing.T) {
	tests := []struct {
		name     string
		req      *http.Request
		expected map[string]interface{}
	}{
		{
			name: "basic GET request",
			req: &http.Request{
				Method: "GET",
				URL: &url.URL{
					Scheme: "https",
					Host:   "example.com",
					Path:   "/api/v1/users",
				},
				Proto: "HTTP/1.1",
				Header: http.Header{
					"User-Agent": []string{"test-agent/1.0"},
				},
			},
			expected: map[string]interface{}{
				"http.request.method":      "GET",
				"url.full":                 "https://example.com/api/v1/users",
				"server.address":           "example.com",
				"url.scheme":               "https",
				"network.protocol.version": "1.1",
				"user_agent.original":      "test-agent/1.0",
			},
		},
		{
			name: "request with non-standard port",
			req: &http.Request{
				Method: "POST",
				URL: &url.URL{
					Scheme: "http",
					Host:   "example.com:8080",
					Path:   "/api/data",
				},
				Proto: "HTTP/2",
			},
			expected: map[string]interface{}{
				"http.request.method":      "POST",
				"url.full":                 "http://example.com:8080/api/data",
				"server.address":           "example.com",
				"server.port":              int64(8080),
				"url.scheme":               "http",
				"network.protocol.version": "2",
			},
		},
		{
			name: "QUERY method",
			req: &http.Request{
				Method: "QUERY",
				URL: &url.URL{
					Scheme: "https",
					Host:   "example.com",
					Path:   "/search",
				},
				Proto: "HTTP/1.1",
			},
			expected: map[string]interface{}{
				"http.request.method": "QUERY",
				"url.scheme":          "https",
			},
		},
	}

	client := NewHTTPClient(nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := client.RequestTraceAttrs(tt.req)

			// Convert to map for easier assertion
			attrMap := make(map[string]interface{})
			for _, attr := range attrs {
				attrMap[string(attr.Key)] = attr.Value.AsInterface()
			}

			for key, expectedVal := range tt.expected {
				actualVal, ok := attrMap[key]
				require.True(t, ok, "expected attribute %s not found", key)
				assert.Equal(t, expectedVal, actualVal, "attribute %s value mismatch", key)
			}
		})
	}
}

func TestHTTPClientResponseTraceAttrs(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantError  bool
	}{
		{
			name:       "2xx success",
			statusCode: 200,
			wantError:  false,
		},
		{
			name:       "4xx client error",
			statusCode: 404,
			wantError:  true,
		},
		{
			name:       "5xx server error",
			statusCode: 500,
			wantError:  true,
		},
	}

	client := NewHTTPClient(nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: tt.statusCode,
			}

			attrs := client.ResponseTraceAttrs(resp)

			// Convert to map for easier assertion
			attrMap := make(map[string]interface{})
			for _, attr := range attrs {
				attrMap[string(attr.Key)] = attr.Value.AsInterface()
			}

			// Check status code attribute
			assert.Equal(t, int64(tt.statusCode), attrMap["http.response.status_code"])

			// Check error.type attribute
			if tt.wantError {
				errorType, hasError := attrMap["error.type"]
				assert.True(t, hasError, "expected error.type attribute for status %d", tt.statusCode)
				assert.NotEmpty(t, errorType)
			}
		})
	}
}

func TestHTTPClientMetrics(t *testing.T) {
	meter := noop.NewMeterProvider().Meter("test")
	client := NewHTTPClient(meter)

	// Verify metrics are initialized
	assert.NotNil(t, client.requestBodySize)
	assert.NotNil(t, client.responseBodySize)
	assert.NotNil(t, client.requestDuration)
	assert.NotNil(t, client.activeRequests)
	assert.NotNil(t, client.openConnections)
	assert.NotNil(t, client.connectionDuration)
}

func TestHTTPClientRecordMetrics(t *testing.T) {
	meter := noop.NewMeterProvider().Meter("test")
	client := NewHTTPClient(meter)

	req := &http.Request{
		Method: "POST",
		URL: &url.URL{
			Scheme: "https",
			Host:   "example.com",
			Path:   "/api/data",
		},
		Proto: "HTTP/1.1",
	}

	// Should not panic
	client.RecordMetrics(
		context.Background(),
		req,
		200,   // statusCode
		1024,  // requestSize
		2048,  // responseSize
		0.123, // elapsedTime
		[]attribute.KeyValue{},
	)
}

func TestHTTPClientStatus(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		expectedCode string
		expectedDesc string
	}{
		{"2xx success", 200, "Unset", ""},
		{"3xx redirect", 301, "Unset", ""},
		{"4xx client error", 404, "Error", ""},
		{"5xx server error", 500, "Error", ""},
		{"invalid code low", 50, "Error", "Invalid HTTP status code 50"},
		{"invalid code high", 600, "Error", "Invalid HTTP status code 600"},
	}

	client := NewHTTPClient(nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, desc := client.Status(tt.statusCode)
			assert.Equal(t, tt.expectedCode, code.String())
			assert.Equal(t, tt.expectedDesc, desc)
		})
	}
}

func TestHTTPClientScheme(t *testing.T) {
	client := NewHTTPClient(nil)

	tests := []struct {
		name     string
		req      *http.Request
		expected string
	}{
		{
			name: "https from URL",
			req: &http.Request{
				URL: &url.URL{Scheme: "https"},
			},
			expected: "https",
		},
		{
			name: "http from URL",
			req: &http.Request{
				URL: &url.URL{Scheme: "http"},
			},
			expected: "http",
		},
		{
			name: "https from TLS",
			req: &http.Request{
				URL: &url.URL{},
				TLS: &tls.ConnectionState{},
			},
			expected: "https",
		},
		{
			name: "http default",
			req: &http.Request{
				URL: &url.URL{},
			},
			expected: "http",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attr := client.traceScheme(tt.req)
			assert.Equal(t, semconv.URLScheme(tt.expected), attr)
		})
	}
}
