// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	grpc_codes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestParseFullMethod(t *testing.T) {
	tests := []struct {
		name           string
		fullMethod     string
		expectedName   string
		expectedAttrs  int
		expectedRPC    bool
		expectedSvc    string
		expectedMethod string
	}{
		{
			name:           "valid full method",
			fullMethod:     "/grpc.testing.TestService/UnaryCall",
			expectedName:   "grpc.testing.TestService/UnaryCall",
			expectedAttrs:  3, // rpc.system, rpc.service, rpc.method
			expectedRPC:    true,
			expectedSvc:    "grpc.testing.TestService",
			expectedMethod: "UnaryCall",
		},
		{
			name:          "no leading slash",
			fullMethod:    "grpc.testing.TestService/UnaryCall",
			expectedName:  "grpc.testing.TestService/UnaryCall",
			expectedAttrs: 1, // only rpc.system
			expectedRPC:   true,
		},
		{
			name:          "no method separator",
			fullMethod:    "/grpc.testing.TestService",
			expectedName:  "grpc.testing.TestService",
			expectedAttrs: 1, // only rpc.system
			expectedRPC:   true,
		},
		{
			name:           "empty service",
			fullMethod:     "//Method",
			expectedName:   "/Method",
			expectedAttrs:  2, // rpc.system, rpc.method
			expectedRPC:    true,
			expectedMethod: "Method",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, attrs := ParseFullMethod(tt.fullMethod)
			assert.Equal(t, tt.expectedName, name)
			assert.Equal(t, tt.expectedAttrs, len(attrs))

			// Check rpc.system is always present
			if tt.expectedRPC {
				assert.Contains(t, attrs, semconv.RPCSystemGRPC)
			}

			// Check service if expected
			if tt.expectedSvc != "" {
				assert.Contains(t, attrs, semconv.RPCService(tt.expectedSvc))
			}

			// Check method if expected
			if tt.expectedMethod != "" {
				assert.Contains(t, attrs, semconv.RPCMethod(tt.expectedMethod))
			}
		})
	}
}

func TestServerStatus(t *testing.T) {
	tests := []struct {
		name         string
		code         int
		message      string
		expectedCode codes.Code
		hasMessage   bool
	}{
		{
			name:         "OK",
			code:         0,
			message:      "",
			expectedCode: codes.Unset,
			hasMessage:   false,
		},
		{
			name:         "Canceled",
			code:         1,
			message:      "canceled",
			expectedCode: codes.Unset,
			hasMessage:   false,
		},
		{
			name:         "Unknown",
			code:         2,
			message:      "unknown error",
			expectedCode: codes.Error,
			hasMessage:   true,
		},
		{
			name:         "InvalidArgument",
			code:         3,
			message:      "invalid",
			expectedCode: codes.Unset,
			hasMessage:   false,
		},
		{
			name:         "DeadlineExceeded",
			code:         4,
			message:      "deadline exceeded",
			expectedCode: codes.Error,
			hasMessage:   true,
		},
		{
			name:         "Internal",
			code:         13,
			message:      "internal error",
			expectedCode: codes.Error,
			hasMessage:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := status.New(grpc_codes.Code(tt.code), tt.message)
			code, msg := ServerStatus(s)
			assert.Equal(t, tt.expectedCode, code)
			if tt.hasMessage {
				assert.Equal(t, tt.message, msg)
			} else {
				assert.Empty(t, msg)
			}
		})
	}
}

func TestClientStatus(t *testing.T) {
	tests := []struct {
		name         string
		code         int
		message      string
		expectedCode codes.Code
		hasMessage   bool
	}{
		{
			name:         "OK",
			code:         0,
			message:      "",
			expectedCode: codes.Unset,
			hasMessage:   false,
		},
		{
			name:         "Canceled",
			code:         1,
			message:      "canceled",
			expectedCode: codes.Error,
			hasMessage:   true,
		},
		{
			name:         "InvalidArgument",
			code:         3,
			message:      "invalid",
			expectedCode: codes.Error,
			hasMessage:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := status.New(grpc_codes.Code(tt.code), tt.message)
			code, msg := ClientStatus(s)
			assert.Equal(t, tt.expectedCode, code)
			if tt.hasMessage {
				assert.Equal(t, tt.message, msg)
			} else {
				assert.Empty(t, msg)
			}
		})
	}
}

func TestServerAddrAttrs(t *testing.T) {
	tests := []struct {
		name         string
		addr         string
		expectedHost string
		expectedPort int
	}{
		{
			name:         "IPv4 with port",
			addr:         "192.168.1.1:8080",
			expectedHost: "192.168.1.1",
			expectedPort: 8080,
		},
		{
			name:         "IPv6 with port",
			addr:         "[::1]:8080",
			expectedHost: "::1",
			expectedPort: 8080,
		},
		{
			name:         "hostname with port",
			addr:         "localhost:50051",
			expectedHost: "localhost",
			expectedPort: 50051,
		},
		{
			name:         "no port",
			addr:         "localhost",
			expectedHost: "localhost",
			expectedPort: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := ServerAddrAttrs(tt.addr)
			if tt.expectedHost != "" {
				assert.Contains(t, attrs, semconv.ServerAddress(tt.expectedHost))
			}
			if tt.expectedPort > 0 {
				assert.Contains(t, attrs, semconv.ServerPort(tt.expectedPort))
			}
		})
	}
}

func TestClientAddrAttrs(t *testing.T) {
	tests := []struct {
		name         string
		addr         string
		expectedHost string
		expectedPort int
	}{
		{
			name:         "IPv4 with port",
			addr:         "192.168.1.100:12345",
			expectedHost: "192.168.1.100",
			expectedPort: 12345,
		},
		{
			name:         "IPv6 with port",
			addr:         "[2001:db8::1]:54321",
			expectedHost: "2001:db8::1",
			expectedPort: 54321,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := ClientAddrAttrs(tt.addr)
			if tt.expectedHost != "" {
				assert.Contains(t, attrs, semconv.ClientAddress(tt.expectedHost))
			}
			if tt.expectedPort > 0 {
				assert.Contains(t, attrs, semconv.ClientPort(tt.expectedPort))
			}
		})
	}
}

func TestIsOTELExporterPath(t *testing.T) {
	tests := []struct {
		name       string
		fullMethod string
		expected   bool
	}{
		{
			name:       "trace exporter path",
			fullMethod: OTELExporterTracePath,
			expected:   true,
		},
		{
			name:       "metric exporter path",
			fullMethod: OTELExporterMetricPath,
			expected:   true,
		},
		{
			name:       "log exporter path",
			fullMethod: OTELExporterLogPath,
			expected:   true,
		},
		{
			name:       "regular method",
			fullMethod: "/grpc.testing.TestService/UnaryCall",
			expected:   false,
		},
		{
			name:       "health check",
			fullMethod: "/grpc.health.v1.Health/Check",
			expected:   false,
		},
		{
			name:       "empty path",
			fullMethod: "",
			expected:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsOTELExporterPath(tt.fullMethod)
			require.Equal(
				t,
				tt.expected,
				result,
				"IsOTELExporterPath(%q) = %v, want %v",
				tt.fullMethod,
				result,
				tt.expected,
			)
		})
	}
}
