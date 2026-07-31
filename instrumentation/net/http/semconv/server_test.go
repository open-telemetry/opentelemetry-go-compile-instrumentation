// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric/noop"
)

func TestHTTPServerRequestTraceAttrs(t *testing.T) {
	tests := []struct {
		name     string
		server   string
		req      *http.Request
		opts     RequestTraceAttrsOpts
		expected map[string]interface{}
	}{
		{
			name:   "basic GET request",
			server: "",
			req: &http.Request{
				Method:     "GET",
				Host:       "example.com",
				RemoteAddr: "192.168.1.1:12345",
				URL: &url.URL{
					Path: "/api/v1/users",
				},
				Proto: "HTTP/1.1",
				Header: http.Header{
					"User-Agent": []string{"test-agent/1.0"},
				},
			},
			expected: map[string]interface{}{
				"http.request.method":      "GET",
				"server.address":           "example.com",
				"url.scheme":               "http",
				"network.peer.address":     "192.168.1.1",
				"network.peer.port":        int64(12345),
				"user_agent.original":      "test-agent/1.0",
				"client.address":           "192.168.1.1",
				"url.path":                 "/api/v1/users",
				"network.protocol.version": "1.1",
			},
		},
		{
			name:   "request with query string",
			server: "",
			req: &http.Request{
				Method:     "GET",
				Host:       "example.com",
				RemoteAddr: "192.168.1.1:12345",
				URL: &url.URL{
					Path:     "/search",
					RawQuery: "q=test&limit=10",
				},
				Proto: "HTTP/1.1",
			},
			expected: map[string]interface{}{
				"url.path":  "/search",
				"url.query": "q=test&limit=10",
			},
		},
		{
			name:   "request with non-standard port",
			server: "example.com:8080",
			req: &http.Request{
				Method:     "POST",
				Host:       "example.com:8080",
				RemoteAddr: "192.168.1.1:12345",
				URL: &url.URL{
					Path: "/api/data",
				},
				Proto: "HTTP/2",
			},
			expected: map[string]interface{}{
				"http.request.method":      "POST",
				"server.address":           "example.com",
				"server.port":              int64(8080),
				"url.scheme":               "http",
				"network.protocol.version": "2",
			},
		},
		{
			name:   "request with X-Forwarded-For",
			server: "",
			req: &http.Request{
				Method:     "GET",
				Host:       "example.com",
				RemoteAddr: "10.0.0.1:12345",
				URL: &url.URL{
					Path: "/api/users",
				},
				Proto: "HTTP/1.1",
				Header: http.Header{
					"X-Forwarded-For": []string{"203.0.113.1, 198.51.100.1"},
				},
			},
			expected: map[string]interface{}{
				"client.address":       "203.0.113.1",
				"network.peer.address": "10.0.0.1",
			},
		},
		{
			name:   "QUERY method",
			server: "",
			req: &http.Request{
				Method:     "QUERY",
				Host:       "example.com",
				RemoteAddr: "192.168.1.1:12345",
				URL: &url.URL{
					Path: "/search",
				},
				Proto: "HTTP/1.1",
			},
			expected: map[string]interface{}{
				"http.request.method": "QUERY",
			},
		},
	}

	server := NewHTTPServer(nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := server.RequestTraceAttrs(tt.server, tt.req, tt.opts)

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

func TestHTTPServerResponseTraceAttrs(t *testing.T) {
	tests := []struct {
		name      string
		resp      ResponseTelemetry
		wantError bool
	}{
		{
			name: "2xx success",
			resp: ResponseTelemetry{
				StatusCode: 200,
				ReadBytes:  1024,
				WriteBytes: 2048,
			},
			wantError: false,
		},
		{
			name: "4xx client error",
			resp: ResponseTelemetry{
				StatusCode: 404,
				WriteBytes: 512,
			},
			wantError: false,
		},
		{
			name: "5xx server error",
			resp: ResponseTelemetry{
				StatusCode: 500,
				WriteBytes: 256,
			},
			wantError: true,
		},
		{
			name: "503 service unavailable",
			resp: ResponseTelemetry{
				StatusCode: 503,
				WriteBytes: 128,
			},
			wantError: true,
		},
	}

	server := NewHTTPServer(nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := server.ResponseTraceAttrs(tt.resp)

			// Convert to map for easier assertion
			attrMap := make(map[string]interface{})
			for _, attr := range attrs {
				attrMap[string(attr.Key)] = attr.Value.AsInterface()
			}

			// Check status code attribute
			if tt.resp.StatusCode > 0 {
				assert.Equal(t, int64(tt.resp.StatusCode), attrMap["http.response.status_code"])
			}

			// Check error.type attribute for 5xx errors
			if tt.wantError {
				errorType, hasError := attrMap["error.type"]
				assert.True(t, hasError, "expected error.type attribute for status %d", tt.resp.StatusCode)
				assert.NotEmpty(t, errorType)
			} else {
				_, hasError := attrMap["error.type"]
				assert.False(t, hasError, "unexpected error.type attribute for status %d", tt.resp.StatusCode)
			}

			// Check body size attributes
			if tt.resp.ReadBytes > 0 {
				assert.Equal(t, int64(tt.resp.ReadBytes), attrMap["http.request.body.size"])
			}
			if tt.resp.WriteBytes > 0 {
				assert.Equal(t, int64(tt.resp.WriteBytes), attrMap["http.response.body.size"])
			}
		})
	}
}

func TestHTTPServerMetrics(t *testing.T) {
	meter := noop.NewMeterProvider().Meter("test")
	server := NewHTTPServer(meter)

	// Verify metrics are initialized
	assert.NotNil(t, server.requestBodySize)
	assert.NotNil(t, server.responseBodySize)
	assert.NotNil(t, server.requestDuration)
	assert.NotNil(t, server.activeRequests)
}

func TestHTTPServerRecordMetrics(t *testing.T) {
	meter := noop.NewMeterProvider().Meter("test")
	server := NewHTTPServer(meter)

	req := &http.Request{
		Method: "POST",
		Host:   "example.com",
		URL: &url.URL{
			Path: "/api/data",
		},
		Proto: "HTTP/1.1",
	}

	// Should not panic
	server.RecordMetrics(
		context.Background(),
		"example.com",
		req,
		200,         // statusCode
		"/api/data", // route
		1024,        // requestSize
		2048,        // responseSize
		0.123,       // elapsedTime
		[]attribute.KeyValue{},
	)
}

func TestHTTPServerStatus(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		expectedCode string
		expectedDesc string
	}{
		{"2xx success", 200, "Unset", ""},
		{"3xx redirect", 301, "Unset", ""},
		{"4xx client error", 404, "Unset", ""},
		{"5xx server error", 500, "Error", ""},
		{"invalid code low", 50, "Error", "Invalid HTTP status code 50"},
		{"invalid code high", 600, "Error", "Invalid HTTP status code 600"},
	}

	server := NewHTTPServer(nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, desc := server.Status(tt.statusCode)
			assert.Equal(t, tt.expectedCode, code.String())
			assert.Equal(t, tt.expectedDesc, desc)
		})
	}
}

func TestHTTPServerRoute(t *testing.T) {
	server := NewHTTPServer(nil)

	tests := []struct {
		route    string
		expected string
	}{
		{"/api/users", "/api/users"},
		{"/api/users/{id}", "/api/users/{id}"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.route, func(t *testing.T) {
			attr := server.Route(tt.route)
			assert.Equal(t, tt.expected, attr.Value.AsString())
		})
	}
}

func TestHTTPServerSpanName(t *testing.T) {
	tests := []struct {
		method   string
		route    string
		expected string
	}{
		{"GET", "/api/users", "GET /api/users"},
		{"POST", "/api/users/{id}", "POST /api/users/{id}"},
		{"GET", "", "GET"},
		{"", "/api/users", " /api/users"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := HTTPServerSpanName(tt.method, tt.route)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHTTPServerNetworkTransportAttr(t *testing.T) {
	server := NewHTTPServer(nil)

	tests := []struct {
		network  string
		expected string
	}{
		{"tcp", "tcp"},
		{"tcp4", "tcp"},
		{"tcp6", "tcp"},
		{"udp", "udp"},
		{"udp4", "udp"},
		{"udp6", "udp"},
		{"unix", "unix"},
		{"unixgram", "unix"},
		{"unixpacket", "unix"},
		{"pipe", "pipe"},
		{"unknown", "pipe"},
	}

	for _, tt := range tests {
		t.Run(tt.network, func(t *testing.T) {
			attrs := server.NetworkTransportAttr(tt.network)
			require.Len(t, attrs, 1)
			assert.Equal(t, "network.transport", string(attrs[0].Key))
			assert.Equal(t, tt.expected, attrs[0].Value.AsString())
		})
	}
}
