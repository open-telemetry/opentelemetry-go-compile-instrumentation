// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package nethttp

import (
	"net"
	"strconv"
	"strings"
)

// netHttpClientAttrsGetter implements HTTP client and network attribute getters
type netHttpClientAttrsGetter struct{}

// GetRequestMethod returns the HTTP request method
func (n netHttpClientAttrsGetter) GetRequestMethod(request *netHttpRequest) string {
	return request.method
}

// GetHTTPRequestHeader returns the HTTP request header values for the given name
func (n netHttpClientAttrsGetter) GetHTTPRequestHeader(request *netHttpRequest, name string) []string {
	return request.header.Values(name)
}

// GetHTTPResponseStatusCode returns the HTTP response status code
func (n netHttpClientAttrsGetter) GetHTTPResponseStatusCode(
	request *netHttpRequest,
	response *netHttpResponse,
	err error,
) int {
	return response.statusCode
}

// GetHTTPResponseHeader returns the HTTP response header values for the given name
func (n netHttpClientAttrsGetter) GetHTTPResponseHeader(
	request *netHttpRequest,
	response *netHttpResponse,
	name string,
) []string {
	return response.header.Values(name)
}

// GetErrorType returns the error type if an error occurred during the request.
// Note: HTTP status code errors are handled separately by HTTPClientSpanStatusExtractor.
// This method only returns error types for transport-level errors (connection errors, timeouts, etc.).
func (n netHttpClientAttrsGetter) GetErrorType(request *netHttpRequest, response *netHttpResponse, err error) string {
	if err != nil {
		// Return the error type name (e.g., "net.OpError", "url.Error")
		// For a more generic approach, we return the error string itself
		return err.Error()
	}
	return ""
}

// GetNetworkType returns the network type (ipv4 or ipv6) based on the host address.
// It attempts to detect IPv6 addresses by parsing the host.
func (n netHttpClientAttrsGetter) GetNetworkType(request *netHttpRequest, response *netHttpResponse) string {
	host := request.host
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
func (n netHttpClientAttrsGetter) GetNetworkTransport(request *netHttpRequest, response *netHttpResponse) string {
	return "tcp"
}

// GetNetworkProtocolName returns the network protocol name
func (n netHttpClientAttrsGetter) GetNetworkProtocolName(request *netHttpRequest, response *netHttpResponse) string {
	if !request.isTls {
		return "http"
	}
	return "https"
}

// GetNetworkProtocolVersion returns the network protocol version
func (n netHttpClientAttrsGetter) GetNetworkProtocolVersion(request *netHttpRequest, response *netHttpResponse) string {
	return request.version
}

// GetNetworkLocalInetAddress returns the local inet address.
// Returns empty string because HTTP client requests do not typically expose
// local socket information. The net/http Client API does not provide access
// to the local address used for outbound connections.
func (n netHttpClientAttrsGetter) GetNetworkLocalInetAddress(
	request *netHttpRequest,
	response *netHttpResponse,
) string {
	return ""
}

// GetNetworkLocalPort returns the local port.
// Returns 0 because HTTP client requests do not typically expose
// local socket information. The net/http Client API does not provide access
// to the local port used for outbound connections.
func (n netHttpClientAttrsGetter) GetNetworkLocalPort(request *netHttpRequest, response *netHttpResponse) int {
	return 0
}

// GetNetworkPeerInetAddress returns the peer inet address
func (n netHttpClientAttrsGetter) GetNetworkPeerInetAddress(request *netHttpRequest, response *netHttpResponse) string {
	return request.host
}

// GetNetworkPeerPort returns the peer port
func (n netHttpClientAttrsGetter) GetNetworkPeerPort(request *netHttpRequest, response *netHttpResponse) int {
	if request.url == nil {
		return 0
	}
	port, err := strconv.Atoi(request.url.Port())
	if err != nil {
		return 0
	}
	return port
}

// GetURLFull returns the full URL string
func (n netHttpClientAttrsGetter) GetURLFull(request *netHttpRequest) string {
	return request.url.String()
}

// GetServerAddress returns the server address
func (n netHttpClientAttrsGetter) GetServerAddress(request *netHttpRequest) string {
	return request.host
}

// GetServerPort returns the server port
func (n netHttpClientAttrsGetter) GetServerPort(request *netHttpRequest) int {
	if request.url == nil {
		return 0
	}
	port, err := strconv.Atoi(request.url.Port())
	if err != nil {
		return 0
	}
	return port
}
