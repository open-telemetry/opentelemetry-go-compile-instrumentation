// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package nethttp

import (
	"strconv"
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
func (n netHttpClientAttrsGetter) GetHTTPResponseStatusCode(request *netHttpRequest, response *netHttpResponse, err error) int {
	return response.statusCode
}

// GetHTTPResponseHeader returns the HTTP response header values for the given name
func (n netHttpClientAttrsGetter) GetHTTPResponseHeader(request *netHttpRequest, response *netHttpResponse, name string) []string {
	return response.header.Values(name)
}

// GetErrorType returns the error type
// TODO: implement status code as error type
func (n netHttpClientAttrsGetter) GetErrorType(request *netHttpRequest, response *netHttpResponse, err error) string {
	return ""
}

// GetNetworkType returns the network type
func (n netHttpClientAttrsGetter) GetNetworkType(request *netHttpRequest, response *netHttpResponse) string {
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

// GetNetworkLocalInetAddress returns the local inet address
func (n netHttpClientAttrsGetter) GetNetworkLocalInetAddress(request *netHttpRequest, response *netHttpResponse) string {
	return ""
}

// GetNetworkLocalPort returns the local port
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
