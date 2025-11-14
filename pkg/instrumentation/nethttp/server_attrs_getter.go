// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package nethttp

import (
	"strconv"
)

// netHttpServerAttrsGetter implements HTTP server, network, and URL attribute getters
type netHttpServerAttrsGetter struct{}

// GetRequestMethod returns the HTTP request method
func (n netHttpServerAttrsGetter) GetRequestMethod(request *netHttpRequest) string {
	return request.method
}

// GetHTTPRequestHeader returns the HTTP request header values for the given name
func (n netHttpServerAttrsGetter) GetHTTPRequestHeader(request *netHttpRequest, name string) []string {
	return request.header.Values(name)
}

// GetHTTPResponseStatusCode returns the HTTP response status code
func (n netHttpServerAttrsGetter) GetHTTPResponseStatusCode(
	request *netHttpRequest,
	response *netHttpResponse,
	err error,
) int {
	return response.statusCode
}

// GetHTTPResponseHeader returns the HTTP response header values for the given name
func (n netHttpServerAttrsGetter) GetHTTPResponseHeader(
	request *netHttpRequest,
	response *netHttpResponse,
	name string,
) []string {
	return response.header.Values(name)
}

// GetErrorType returns the error type
// TODO: implement status code as error type
func (n netHttpServerAttrsGetter) GetErrorType(request *netHttpRequest, response *netHttpResponse, err error) string {
	return ""
}

// GetURLScheme returns the URL scheme
func (n netHttpServerAttrsGetter) GetURLScheme(request *netHttpRequest) string {
	if request.url.Scheme != "" {
		return request.url.Scheme
	}
	return n.GetNetworkProtocolName(request, &netHttpResponse{})
}

// GetURLPath returns the URL path
func (n netHttpServerAttrsGetter) GetURLPath(request *netHttpRequest) string {
	return request.url.Path
}

// GetURLQuery returns the URL query string
func (n netHttpServerAttrsGetter) GetURLQuery(request *netHttpRequest) string {
	return request.url.RawQuery
}

// GetNetworkType returns the network type
func (n netHttpServerAttrsGetter) GetNetworkType(request *netHttpRequest, response *netHttpResponse) string {
	return "ipv4"
}

// GetNetworkTransport returns the network transport protocol
func (n netHttpServerAttrsGetter) GetNetworkTransport(request *netHttpRequest, response *netHttpResponse) string {
	return "tcp"
}

// GetNetworkProtocolName returns the network protocol name
func (n netHttpServerAttrsGetter) GetNetworkProtocolName(request *netHttpRequest, response *netHttpResponse) string {
	if !request.isTls {
		return "http"
	}
	return "https"
}

// GetNetworkProtocolVersion returns the network protocol version
func (n netHttpServerAttrsGetter) GetNetworkProtocolVersion(request *netHttpRequest, response *netHttpResponse) string {
	return request.version
}

// GetNetworkLocalInetAddress returns the local inet address
func (n netHttpServerAttrsGetter) GetNetworkLocalInetAddress(
	request *netHttpRequest,
	response *netHttpResponse,
) string {
	return ""
}

// GetNetworkLocalPort returns the local port
func (n netHttpServerAttrsGetter) GetNetworkLocalPort(request *netHttpRequest, response *netHttpResponse) int {
	return 0
}

// GetNetworkPeerInetAddress returns the peer inet address
func (n netHttpServerAttrsGetter) GetNetworkPeerInetAddress(request *netHttpRequest, response *netHttpResponse) string {
	return request.host
}

// GetNetworkPeerPort returns the peer port
func (n netHttpServerAttrsGetter) GetNetworkPeerPort(request *netHttpRequest, response *netHttpResponse) int {
	if request.url == nil {
		return 0
	}
	port, err := strconv.Atoi(request.url.Port())
	if err != nil {
		return 0
	}
	return port
}

// GetHTTPRoute returns the HTTP route
func (n netHttpServerAttrsGetter) GetHTTPRoute(request *netHttpRequest) string {
	return request.url.Path
}
