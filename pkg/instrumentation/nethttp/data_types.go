// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package nethttp

import (
	"net/http"
	"net/url"
	"strconv"
)

// NetHttpRequest represents an HTTP request with extracted attributes
type NetHttpRequest struct {
	method  string
	url     *url.URL
	host    string
	isTls   bool
	header  http.Header
	version string
}

// NewNetHttpRequest creates a new NetHttpRequest
func NewNetHttpRequest(
	method string,
	url *url.URL,
	host string,
	header http.Header,
	version string,
	isTls bool,
) *NetHttpRequest {
	return &NetHttpRequest{
		method:  method,
		url:     url,
		host:    host,
		header:  header,
		version: version,
		isTls:   isTls,
	}
}

// Method returns the HTTP request method
func (r *NetHttpRequest) Method() string {
	return r.method
}

// URL returns the request URL
func (r *NetHttpRequest) URL() *url.URL {
	return r.url
}

// Host returns the request host
func (r *NetHttpRequest) Host() string {
	return r.host
}

// IsTls returns whether the request uses TLS
func (r *NetHttpRequest) IsTls() bool {
	return r.isTls
}

// Header returns the request headers
func (r *NetHttpRequest) Header() http.Header {
	return r.header
}

// Version returns the HTTP version
func (r *NetHttpRequest) Version() string {
	return r.version
}

// NetHttpResponse represents an HTTP response with extracted attributes
type NetHttpResponse struct {
	statusCode int
	header     http.Header
}

// NewNetHttpResponse creates a new NetHttpResponse
func NewNetHttpResponse(statusCode int, header http.Header) *NetHttpResponse {
	return &NetHttpResponse{
		statusCode: statusCode,
		header:     header,
	}
}

// StatusCode returns the HTTP response status code
func (r *NetHttpResponse) StatusCode() int {
	return r.statusCode
}

// Header returns the response headers
func (r *NetHttpResponse) Header() http.Header {
	return r.header
}

// GetProtocolVersion converts HTTP major and minor version numbers to a string
func GetProtocolVersion(majorVersion, minorVersion int) string {
	if majorVersion == 1 && minorVersion == 0 {
		return "1.0"
	} else if majorVersion == 1 && minorVersion == 1 {
		return "1.1"
	} else if majorVersion == 1 && minorVersion == 2 {
		return "1.2"
	} else if majorVersion == 2 {
		return "2"
	} else if majorVersion == 3 {
		return "3"
	}
	return strconv.Itoa(majorVersion) + "." + strconv.Itoa(minorVersion)
}
