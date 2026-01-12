// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"testing"

	"go.opentelemetry.io/collector/pdata/ptrace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

// RequireHTTPClientSemconv verifies that an HTTP client span follows semantic conventions.
// Reference: https://opentelemetry.io/docs/specs/semconv/http/http-spans/#http-client-span
func RequireHTTPClientSemconv(t *testing.T, span ptrace.Span, method, urlFull, serverAddress string, statusCode int64) {
	// Required attributes - all validated with exact values
	RequireAttribute(t, span, string(semconv.HTTPRequestMethodKey), method)
	RequireAttribute(t, span, string(semconv.ServerAddressKey), serverAddress)
	RequireAttribute(t, span, string(semconv.URLFullKey), urlFull)
	// Conditionally required (when response is received)
	RequireAttribute(t, span, string(semconv.HTTPResponseStatusCodeKey), statusCode)
	// Recommended attributes
	RequireAttributeExists(t, span, string(semconv.NetworkProtocolVersionKey))
	RequireAttributeExists(t, span, string(semconv.URLSchemeKey))
	RequireAttributeExists(t, span, string(semconv.ServerPortKey))
}

// RequireHTTPServerSemconv verifies that an HTTP server span follows semantic conventions.
// Reference: https://opentelemetry.io/docs/specs/semconv/http/http-spans/#http-server-span
func RequireHTTPServerSemconv(t *testing.T, span ptrace.Span, method, urlPath, urlScheme string, statusCode int64) {
	// Required attributes - all validated with exact values
	RequireAttribute(t, span, string(semconv.HTTPRequestMethodKey), method)
	RequireAttribute(t, span, string(semconv.URLPathKey), urlPath)
	RequireAttribute(t, span, string(semconv.URLSchemeKey), urlScheme)
	// Conditionally required (when response is sent)
	RequireAttribute(t, span, string(semconv.HTTPResponseStatusCodeKey), statusCode)
	// Recommended attributes
	RequireAttributeExists(t, span, string(semconv.ClientAddressKey))
	RequireAttributeExists(t, span, string(semconv.UserAgentOriginalKey))
	RequireAttributeExists(t, span, string(semconv.NetworkProtocolVersionKey))
	RequireAttributeExists(t, span, string(semconv.ServerAddressKey))
	RequireAttributeExists(t, span, string(semconv.ServerPortKey))
}

// RequireGRPCClientSemconv verifies that a gRPC client span follows semantic conventions.
// Reference: https://opentelemetry.io/docs/specs/semconv/rpc/rpc-spans/
func RequireGRPCClientSemconv(t *testing.T, span ptrace.Span, serverAddress string) {
	// Required attributes - all validated with exact values
	RequireAttribute(t, span, string(semconv.RPCSystemKey), "grpc")
	RequireAttribute(t, span, string(semconv.ServerAddressKey), serverAddress)
	// Recommended attributes
	RequireAttributeExists(t, span, string(semconv.RPCServiceKey))
	RequireAttributeExists(t, span, string(semconv.RPCMethodKey))
	// Conditionally required (when server responds)
	RequireAttributeExists(t, span, string(semconv.RPCGRPCStatusCodeKey))
}

// RequireGRPCServerSemconv verifies that a gRPC server span follows semantic conventions.
// Reference: https://opentelemetry.io/docs/specs/semconv/rpc/rpc-spans/
func RequireGRPCServerSemconv(t *testing.T, span ptrace.Span) {
	// Required attributes
	RequireAttribute(t, span, string(semconv.RPCSystemKey), "grpc")
	// Recommended attributes
	RequireAttributeExists(t, span, string(semconv.RPCServiceKey))
	RequireAttributeExists(t, span, string(semconv.RPCMethodKey))
	// Conditionally required (when response is sent)
	RequireAttributeExists(t, span, string(semconv.RPCGRPCStatusCodeKey))
}
