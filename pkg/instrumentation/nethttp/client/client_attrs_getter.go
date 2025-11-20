// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"net"
	"strconv"
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/nethttp"
)

// netHttpClientAttrsGetter implements HTTP client and network attribute getters
type netHttpClientAttrsGetter struct{}

// GetRequestMethod returns the HTTP request method
func (n netHttpClientAttrsGetter) GetRequestMethod(request *nethttp.NetHttpRequest) string {
	return request.Method()
}

// GetHTTPRequestHeader returns the HTTP request header values for the given name
func (n netHttpClientAttrsGetter) GetHTTPRequestHeader(request *nethttp.NetHttpRequest, name string) []string {
	return request.Header().Values(name)
}

// GetHTTPResponseStatusCode returns the HTTP response status code
func (n netHttpClientAttrsGetter) GetHTTPResponseStatusCode(
	request *nethttp.NetHttpRequest,
	response *nethttp.NetHttpResponse,
	err error,
) int {
	return response.StatusCode()
}

// GetHTTPResponseHeader returns the HTTP response header values for the given name
func (n netHttpClientAttrsGetter) GetHTTPResponseHeader(
	request *nethttp.NetHttpRequest,
	response *nethttp.NetHttpResponse,
	name string,
) []string {
	return response.Header().Values(name)
}

// GetErrorType returns the error type if an error occurred during the request.
// Note: HTTP status code errors are handled separately by HTTPClientSpanStatusExtractor.
// This method only returns error types for transport-level errors (connection errors, timeouts, etc.).
func (n netHttpClientAttrsGetter) GetErrorType(
	request *nethttp.NetHttpRequest,
	response *nethttp.NetHttpResponse,
	err error,
) string {
	if err != nil {
		// Return the error type name (e.g., "net.OpError", "url.Error")
		// For a more generic approach, we return the error string itself
		return err.Error()
	}
	return ""
}

// GetNetworkType returns the network type (ipv4 or ipv6) based on the host address.
// It attempts to detect IPv6 addresses by parsing the host.
func (n netHttpClientAttrsGetter) GetNetworkType(
	request *nethttp.NetHttpRequest,
	response *nethttp.NetHttpResponse,
) string {
	host := request.Host()
	if host == "" {
		return "ipv4" // default to ipv4 if host is not available
	}

	// Remove brackets if present (for IPv6 addresses)
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = strings.Trim(host, "[]")
	}

	// Try to split host:port to extract just the host
	if hostPart, _, err := net.SplitHostPort(host); err == nil {
		host = hostPart
	}
	// If SplitHostPort fails, host might be an IP address or hostname without port

	// Try to parse as IP address
	ip := net.ParseIP(host)
	if ip != nil {
		if ip.To4() != nil {
			return "ipv4"
		}
		return "ipv6"
	}

	// For hostnames, default to ipv4
	return "ipv4"
}

// GetNetworkTransport returns the network transport protocol
func (n netHttpClientAttrsGetter) GetNetworkTransport(
	request *nethttp.NetHttpRequest,
	response *nethttp.NetHttpResponse,
) string {
	return "tcp"
}

// GetNetworkProtocolName returns the network protocol name
func (n netHttpClientAttrsGetter) GetNetworkProtocolName(
	request *nethttp.NetHttpRequest,
	response *nethttp.NetHttpResponse,
) string {
	if !request.IsTls() {
		return "http"
	}
	return "https"
}

// GetNetworkProtocolVersion returns the network protocol version
func (n netHttpClientAttrsGetter) GetNetworkProtocolVersion(
	request *nethttp.NetHttpRequest,
	response *nethttp.NetHttpResponse,
) string {
	return request.Version()
}

// GetNetworkLocalInetAddress returns the local inet address.
// Returns empty string because HTTP client requests do not typically expose
// local socket information. The net/http Client API does not provide access
// to the local address used for outbound connections.
func (n netHttpClientAttrsGetter) GetNetworkLocalInetAddress(
	request *nethttp.NetHttpRequest,
	response *nethttp.NetHttpResponse,
) string {
	return ""
}

// GetNetworkLocalPort returns the local port.
// Returns 0 because HTTP client requests do not typically expose
// local socket information. The net/http Client API does not provide access
// to the local port used for outbound connections.
func (n netHttpClientAttrsGetter) GetNetworkLocalPort(
	request *nethttp.NetHttpRequest,
	response *nethttp.NetHttpResponse,
) int {
	return 0
}

// GetNetworkPeerInetAddress returns the peer inet address
func (n netHttpClientAttrsGetter) GetNetworkPeerInetAddress(
	request *nethttp.NetHttpRequest,
	response *nethttp.NetHttpResponse,
) string {
	return request.Host()
}

// GetNetworkPeerPort returns the peer port
func (n netHttpClientAttrsGetter) GetNetworkPeerPort(
	request *nethttp.NetHttpRequest,
	response *nethttp.NetHttpResponse,
) int {
	if request.URL() == nil {
		return 0
	}
	port, err := strconv.Atoi(request.URL().Port())
	if err != nil {
		return 0
	}
	return port
}

// GetURLFull returns the full URL string
func (n netHttpClientAttrsGetter) GetURLFull(request *nethttp.NetHttpRequest) string {
	return request.URL().String()
}

// GetServerAddress returns the server address
func (n netHttpClientAttrsGetter) GetServerAddress(request *nethttp.NetHttpRequest) string {
	return request.Host()
}

// GetServerPort returns the server port
func (n netHttpClientAttrsGetter) GetServerPort(request *nethttp.NetHttpRequest) int {
	if request.URL() == nil {
		return 0
	}
	port, err := strconv.Atoi(request.URL().Port())
	if err != nil {
		return 0
	}
	return port
}
