// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"strconv"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/nethttp"
)

// netHttpServerAttrsGetter implements HTTP server, network, and URL attribute getters
type netHttpServerAttrsGetter struct{}

// GetRequestMethod returns the HTTP request method
func (n netHttpServerAttrsGetter) GetRequestMethod(request *nethttp.NetHttpRequest) string {
	return request.Method()
}

// GetHTTPRequestHeader returns the HTTP request header values for the given name
func (n netHttpServerAttrsGetter) GetHTTPRequestHeader(request *nethttp.NetHttpRequest, name string) []string {
	return request.Header().Values(name)
}

// GetHTTPResponseStatusCode returns the HTTP response status code
func (n netHttpServerAttrsGetter) GetHTTPResponseStatusCode(
	request *nethttp.NetHttpRequest,
	response *nethttp.NetHttpResponse,
	err error,
) int {
	return response.StatusCode()
}

// GetHTTPResponseHeader returns the HTTP response header values for the given name
func (n netHttpServerAttrsGetter) GetHTTPResponseHeader(
	request *nethttp.NetHttpRequest,
	response *nethttp.NetHttpResponse,
	name string,
) []string {
	return response.Header().Values(name)
}

// GetErrorType returns the error type
// TODO: implement status code as error type
func (n netHttpServerAttrsGetter) GetErrorType(
	request *nethttp.NetHttpRequest,
	response *nethttp.NetHttpResponse,
	err error,
) string {
	return ""
}

// GetURLScheme returns the URL scheme
func (n netHttpServerAttrsGetter) GetURLScheme(request *nethttp.NetHttpRequest) string {
	if request.URL().Scheme != "" {
		return request.URL().Scheme
	}
	return n.GetNetworkProtocolName(request, &nethttp.NetHttpResponse{})
}

// GetURLPath returns the URL path
func (n netHttpServerAttrsGetter) GetURLPath(request *nethttp.NetHttpRequest) string {
	return request.URL().Path
}

// GetURLQuery returns the URL query string
func (n netHttpServerAttrsGetter) GetURLQuery(request *nethttp.NetHttpRequest) string {
	return request.URL().RawQuery
}

// GetNetworkType returns the network type
func (n netHttpServerAttrsGetter) GetNetworkType(
	request *nethttp.NetHttpRequest,
	response *nethttp.NetHttpResponse,
) string {
	return "ipv4"
}

// GetNetworkTransport returns the network transport protocol
func (n netHttpServerAttrsGetter) GetNetworkTransport(
	request *nethttp.NetHttpRequest,
	response *nethttp.NetHttpResponse,
) string {
	return "tcp"
}

// GetNetworkProtocolName returns the network protocol name
func (n netHttpServerAttrsGetter) GetNetworkProtocolName(
	request *nethttp.NetHttpRequest,
	response *nethttp.NetHttpResponse,
) string {
	if !request.IsTls() {
		return "http"
	}
	return "https"
}

// GetNetworkProtocolVersion returns the network protocol version
func (n netHttpServerAttrsGetter) GetNetworkProtocolVersion(
	request *nethttp.NetHttpRequest,
	response *nethttp.NetHttpResponse,
) string {
	return request.Version()
}

// GetNetworkLocalInetAddress returns the local inet address
func (n netHttpServerAttrsGetter) GetNetworkLocalInetAddress(
	request *nethttp.NetHttpRequest,
	response *nethttp.NetHttpResponse,
) string {
	return ""
}

// GetNetworkLocalPort returns the local port
func (n netHttpServerAttrsGetter) GetNetworkLocalPort(
	request *nethttp.NetHttpRequest,
	response *nethttp.NetHttpResponse,
) int {
	return 0
}

// GetNetworkPeerInetAddress returns the peer inet address
func (n netHttpServerAttrsGetter) GetNetworkPeerInetAddress(
	request *nethttp.NetHttpRequest,
	response *nethttp.NetHttpResponse,
) string {
	return request.Host()
}

// GetNetworkPeerPort returns the peer port
func (n netHttpServerAttrsGetter) GetNetworkPeerPort(
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

// GetHTTPRoute returns the HTTP route
func (n netHttpServerAttrsGetter) GetHTTPRoute(request *nethttp.NetHttpRequest) string {
	return request.URL().Path
}
