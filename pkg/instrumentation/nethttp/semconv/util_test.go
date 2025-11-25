// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
		{"QUERY", "QUERY"},
		{"query", "QUERY"},
		{"CUSTOM", "_OTHER"},
		{"", "_OTHER"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := StandardizeHTTPMethod(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
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
		{"QUERY", true},
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
