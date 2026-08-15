// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
)

func TestSplitHostPort(t *testing.T) {
	tests := []struct {
		name         string
		hostport     string
		expectedHost string
		expectedPort int
	}{
		{"host only", "example.com", "example.com", -1},
		{"host:port", "example.com:8080", "example.com", 8080},
		{"IPv4", "192.168.1.1", "192.168.1.1", -1},
		{"IPv4:port", "192.168.1.1:8080", "192.168.1.1", 8080},
		{"IPv6 brackets", "[::1]", "::1", -1},
		{"IPv6 brackets:port", "[::1]:8080", "::1", 8080},
		{"port only", ":8080", "", 8080},
		{"empty", "", "", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, port := SplitHostPort(tt.hostport)
			assert.Equal(t, tt.expectedHost, host)
			assert.Equal(t, tt.expectedPort, port)
		})
	}
}

func TestRequiredHTTPPort(t *testing.T) {
	tests := []struct {
		name     string
		https    bool
		port     int
		expected int
	}{
		{"HTTP default port", false, 80, -1},
		{"HTTP non-default", false, 8080, 8080},
		{"HTTPS default port", true, 443, -1},
		{"HTTPS non-default", true, 8443, 8443},
		{"zero port HTTP", false, 0, -1},
		{"zero port HTTPS", true, 0, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RequiredHTTPPort(tt.https, tt.port)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNetProtocol(t *testing.T) {
	tests := []struct {
		proto           string
		expectedName    string
		expectedVersion string
	}{
		{"HTTP/1.1", "http", "1.1"},
		{"HTTP/2", "http", "2"},
		{"HTTP/3", "http", "3"},
		{"QUIC/1", "quic", "1"},
		{"SPDY/3", "spdy", "3"},
	}

	for _, tt := range tests {
		t.Run(tt.proto, func(t *testing.T) {
			name, version := NetProtocol(tt.proto)
			assert.Equal(t, tt.expectedName, name)
			assert.Equal(t, tt.expectedVersion, version)
		})
	}
}

func TestStandardizeHTTPMethod(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"GET", "GET"},
		{"get", "GET"},
		{"Post", "POST"},
		{"QUERY", HTTPMethodOther},
		{"query", HTTPMethodOther},
		{"CUSTOM", HTTPMethodOther},
		{"", HTTPMethodOther},
		{HTTPMethodOther, HTTPMethodOther},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := StandardizeHTTPMethod(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseKnownMethods(t *testing.T) {
	got := parseKnownMethods("GET, PROPFIND,POST")
	assert.Equal(t, "GET", got["GET"].Value.AsString())
	assert.Equal(t, "PROPFIND", got["PROPFIND"].Value.AsString())
	assert.Equal(t, "POST", got["POST"].Value.AsString())
	_, hasPut := got["PUT"]
	assert.False(t, hasPut, "override must fully replace defaults")

	empty := parseKnownMethods(" , , ")
	assert.Empty(t, empty)
}

func resetKnownMethodsForTest(t *testing.T) {
	t.Helper()
	knownMethodsOnce = sync.Once{}
	knownMethodsMap = nil
}

func TestKnownMethods(t *testing.T) {
	t.Cleanup(func() {
		t.Setenv(HTTPKnownMethodsEnv, "")
		resetKnownMethodsForTest(t)
	})

	t.Run("defaults", func(t *testing.T) {
		t.Setenv(HTTPKnownMethodsEnv, "")
		resetKnownMethodsForTest(t)

		got := knownMethods()
		assert.Equal(t, MethodLookup, got)
		_, hasQuery := got["QUERY"]
		assert.False(t, hasQuery, "QUERY is not a default known method")
	})

	t.Run("env override", func(t *testing.T) {
		t.Setenv(HTTPKnownMethodsEnv, "GET,QUERY,PROPFIND")
		resetKnownMethodsForTest(t)

		got := knownMethods()
		assert.Equal(t, "GET", got["GET"].Value.AsString())
		assert.Equal(t, "QUERY", got["QUERY"].Value.AsString())
		assert.Equal(t, "PROPFIND", got["PROPFIND"].Value.AsString())
		_, hasPost := got["POST"]
		assert.False(t, hasPost, "override must fully replace defaults")

		assert.Equal(t, "QUERY", StandardizeHTTPMethod("QUERY"))
		assert.Equal(t, "QUERY", SpanMethod("QUERY"))
	})
}

func TestRequestMethodAttrs(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		wantMethod     string
		wantOriginal   string
		wantNoOriginal bool
	}{
		{name: "known", method: "GET", wantMethod: "GET", wantNoOriginal: true},
		{name: "case variant", method: "get", wantMethod: "GET", wantOriginal: "get"},
		{name: "unknown", method: "CUSTOM", wantMethod: HTTPMethodOther, wantOriginal: "CUSTOM"},
		{name: "QUERY not default", method: "QUERY", wantMethod: HTTPMethodOther, wantOriginal: "QUERY"},
		{name: "empty", method: "", wantMethod: HTTPMethodOther, wantNoOriginal: true},
		{name: "literal _OTHER", method: HTTPMethodOther, wantMethod: HTTPMethodOther, wantNoOriginal: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method, original := requestMethodAttrs(tt.method)
			assert.Equal(t, "http.request.method", string(method.Key))
			assert.Equal(t, tt.wantMethod, method.Value.AsString())
			if tt.wantNoOriginal {
				assert.Equal(t, attribute.KeyValue{}, original)
				return
			}
			assert.Equal(t, "http.request.method_original", string(original.Key))
			assert.Equal(t, tt.wantOriginal, original.Value.AsString())
		})
	}
}

func TestSpanMethod(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"GET", "GET"},
		{"get", "GET"},
		{"QUERY", "HTTP"},
		{"CUSTOM", "HTTP"},
		{"", "HTTP"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, SpanMethod(tt.input))
		})
	}
}

func TestHTTPClientSpanName(t *testing.T) {
	assert.Equal(t, "POST", HTTPClientSpanName("POST"))
	assert.Equal(t, "HTTP", HTTPClientSpanName("CUSTOM"))
	assert.Equal(t, "HTTP", HTTPClientSpanName(""))
}

func TestMethodLookup(t *testing.T) {
	tests := []struct {
		method string
		exists bool
	}{
		{"GET", true},
		{"POST", true},
		{"PUT", true},
		{"DELETE", true},
		{"PATCH", true},
		{"HEAD", true},
		{"OPTIONS", true},
		{"CONNECT", true},
		{"TRACE", true},
		{"QUERY", false},
		{"CUSTOM", false},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			_, exists := MethodLookup[tt.method]
			assert.Equal(t, tt.exists, exists)
		})
	}
}

func TestHTTPRoute(t *testing.T) {
	tests := []struct {
		pattern  string
		expected string
	}{
		{"GET /api/users", "/api/users"},
		{"/api/users", "/api/users"},
		{"", ""},
		{"GET", ""},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			result := HTTPRoute(tt.pattern)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestServerClientIP(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"192.168.1.1", "192.168.1.1"},
		{"192.168.1.1, 10.0.0.1", "192.168.1.1"},
		{"192.168.1.1, 10.0.0.1, 172.16.0.1", "192.168.1.1"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ServerClientIP(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
