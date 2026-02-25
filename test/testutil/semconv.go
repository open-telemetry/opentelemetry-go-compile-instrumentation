// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"net"
	"strconv"
	"testing"

	"go.opentelemetry.io/collector/pdata/ptrace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

// RequireHTTPClientSemconv verifies that an HTTP client span follows semantic conventions.
// Reference: https://opentelemetry.io/docs/specs/semconv/http/http-spans/#http-client-span
func RequireHTTPClientSemconv(
	t *testing.T,
	span ptrace.Span,
	method, urlFull, serverAddress string,
	statusCode, serverPort int64,
	networkProtocolVersion, urlScheme string,
) {
	// Required attributes - all validated with exact values
	RequireAttribute(t, span, string(semconv.HTTPRequestMethodKey), method)
	RequireAttribute(t, span, string(semconv.ServerAddressKey), serverAddress)
	RequireAttribute(t, span, string(semconv.URLFullKey), urlFull)
	// Conditionally required (when response is received)
	RequireAttribute(t, span, string(semconv.HTTPResponseStatusCodeKey), statusCode)
	// Recommended attributes - all validated with exact values
	RequireAttribute(t, span, string(semconv.NetworkProtocolVersionKey), networkProtocolVersion)
	RequireAttribute(t, span, string(semconv.URLSchemeKey), urlScheme)
	RequireAttribute(t, span, string(semconv.ServerPortKey), serverPort)
}

// RequireHTTPServerSemconv verifies that an HTTP server span follows semantic conventions.
// Reference: https://opentelemetry.io/docs/specs/semconv/http/http-spans/#http-server-span
func RequireHTTPServerSemconv(
	t *testing.T,
	span ptrace.Span,
	method, urlPath, urlScheme string,
	statusCode, serverPort int64,
	clientAddress, userAgent, networkProtocolVersion, serverAddress string,
) {
	// Required attributes - all validated with exact values
	RequireAttribute(t, span, string(semconv.HTTPRequestMethodKey), method)
	RequireAttribute(t, span, string(semconv.URLPathKey), urlPath)
	RequireAttribute(t, span, string(semconv.URLSchemeKey), urlScheme)
	// Conditionally required (when response is sent)
	RequireAttribute(t, span, string(semconv.HTTPResponseStatusCodeKey), statusCode)
	// Recommended attributes - all validated with exact values
	RequireAttribute(t, span, string(semconv.ClientAddressKey), clientAddress)
	RequireAttribute(t, span, string(semconv.UserAgentOriginalKey), userAgent)
	RequireAttribute(t, span, string(semconv.NetworkProtocolVersionKey), networkProtocolVersion)
	RequireAttribute(t, span, string(semconv.ServerAddressKey), serverAddress)
	RequireAttribute(t, span, string(semconv.ServerPortKey), serverPort)
}

// RequireGRPCClientSemconv verifies that a gRPC client span follows semantic conventions.
// Reference: https://opentelemetry.io/docs/specs/semconv/rpc/rpc-spans/
func RequireGRPCClientSemconv(
	t *testing.T,
	span ptrace.Span,
	serverAddress, rpcService, rpcMethod string,
	grpcStatusCode int64,
) {
	// Required attributes - all validated with exact values
	RequireAttribute(t, span, string(semconv.RPCSystemKey), "grpc")
	RequireAttribute(t, span, string(semconv.ServerAddressKey), serverAddress)
	// Recommended attributes - all validated with exact values
	RequireAttribute(t, span, string(semconv.RPCServiceKey), rpcService)
	RequireAttribute(t, span, string(semconv.RPCMethodKey), rpcMethod)
	// Conditionally required (when server responds) - validated with exact value
	RequireAttribute(t, span, string(semconv.RPCGRPCStatusCodeKey), grpcStatusCode)
}

// RequireDBClientSemconv verifies that a database client span follows semantic conventions.
// Reference: https://opentelemetry.io/docs/specs/semconv/database/database-spans/
func RequireDBClientSemconv(
	t *testing.T,
	span ptrace.Span,
	dbOperationName, dbQueryText, serverAddress string,
	serverPort int64,
	dbNamespace string,
) {
	// Required attributes
	RequireAttribute(t, span, string(semconv.DBOperationNameKey), dbOperationName)
	// Recommended attributes
	RequireAttribute(t, span, string(semconv.DBQueryTextKey), dbQueryText)
	RequireAttribute(t, span, string(semconv.ServerAddressKey), serverAddress)
	if serverPort > 0 {
		RequireAttribute(t, span, string(semconv.ServerPortKey), serverPort)
	}
	RequireAttribute(t, span, string(semconv.DBNamespaceKey), dbNamespace)
}

// RequireGRPCServerSemconv verifies that a gRPC server span follows semantic conventions.
// Reference: https://opentelemetry.io/docs/specs/semconv/rpc/rpc-spans/
func RequireGRPCServerSemconv(t *testing.T, span ptrace.Span, rpcService, rpcMethod string, grpcStatusCode int64) {
	// Required attributes - all validated with exact values
	RequireAttribute(t, span, string(semconv.RPCSystemKey), "grpc")
	// Recommended attributes - all validated with exact values
	RequireAttribute(t, span, string(semconv.RPCServiceKey), rpcService)
	RequireAttribute(t, span, string(semconv.RPCMethodKey), rpcMethod)
	// Conditionally required (when response is sent) - validated with exact value
	RequireAttribute(t, span, string(semconv.RPCGRPCStatusCodeKey), grpcStatusCode)
}

// RequireRedisClientSemconv verifies that a Redis client span follows semantic conventions.
// Reference: https://opentelemetry.io/docs/specs/semconv/database/redis/
func RequireRedisClientSemconv(
	t *testing.T,
	span ptrace.Span,
	operationName, endpoint, queryText string,
) {
	host, portStr, err := net.SplitHostPort(endpoint)
	if err != nil {
		host = endpoint
	}

	RequireAttribute(t, span, string(semconv.DBSystemNameKey), "redis")
	RequireAttribute(t, span, string(semconv.DBOperationNameKey), operationName)
	RequireAttribute(t, span, string(semconv.ServerAddressKey), host)
	RequireAttribute(t, span, string(semconv.NetworkTransportKey), "tcp")
	RequireAttribute(t, span, string(semconv.DBQueryTextKey), queryText)

	if err == nil {
		if port, convErr := strconv.Atoi(portStr); convErr == nil && port > 0 {
			RequireAttribute(t, span, string(semconv.ServerPortKey), int64(port))
		}
	}
}
