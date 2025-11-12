// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package nethttp

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestServerAttrsGetter_GetRequestMethod(t *testing.T) {
	getter := &netHttpServerAttrsGetter{}
	req := &netHttpRequest{method: "POST"}
	assert.Equal(t, "POST", getter.GetRequestMethod(req))
}

func TestServerAttrsGetter_GetHTTPRequestHeader(t *testing.T) {
	getter := &netHttpServerAttrsGetter{}
	header := http.Header{}
	header.Set("Content-Type", "application/json")
	header.Add("Accept", "application/json")
	header.Add("Accept", "text/html")

	req := &netHttpRequest{header: header}

	contentType := getter.GetHTTPRequestHeader(req, "Content-Type")
	assert.Equal(t, []string{"application/json"}, contentType)

	accept := getter.GetHTTPRequestHeader(req, "Accept")
	assert.Equal(t, []string{"application/json", "text/html"}, accept)

	notFound := getter.GetHTTPRequestHeader(req, "Not-Exist")
	assert.Empty(t, notFound)
}

func TestServerAttrsGetter_GetHTTPResponseStatusCode(t *testing.T) {
	getter := &netHttpServerAttrsGetter{}
	req := &netHttpRequest{}

	tests := []struct {
		name       string
		resp       *netHttpResponse
		err        error
		wantStatus int
	}{
		{"OK", &netHttpResponse{statusCode: 200}, nil, 200},
		{"Not Found", &netHttpResponse{statusCode: 404}, nil, 404},
		{"Server Error", &netHttpResponse{statusCode: 500}, nil, 500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := getter.GetHTTPResponseStatusCode(req, tt.resp, tt.err)
			assert.Equal(t, tt.wantStatus, status)
		})
	}
}

func TestServerAttrsGetter_GetHTTPResponseHeader(t *testing.T) {
	getter := &netHttpServerAttrsGetter{}
	req := &netHttpRequest{}
	header := http.Header{}
	header.Set("Content-Type", "application/json")
	header.Add("X-Custom", "value1")
	header.Add("X-Custom", "value2")

	resp := &netHttpResponse{header: header}

	contentType := getter.GetHTTPResponseHeader(req, resp, "Content-Type")
	assert.Equal(t, []string{"application/json"}, contentType)

	custom := getter.GetHTTPResponseHeader(req, resp, "X-Custom")
	assert.Equal(t, []string{"value1", "value2"}, custom)
}

func TestServerAttrsGetter_GetErrorType(t *testing.T) {
	getter := &netHttpServerAttrsGetter{}
	req := &netHttpRequest{}
	resp := &netHttpResponse{statusCode: 500}

	// Currently returns empty string (TODO in loongsuite implementation)
	errorType := getter.GetErrorType(req, resp, nil)
	assert.Equal(t, "", errorType)
}

func TestServerAttrsGetter_GetURLScheme(t *testing.T) {
	getter := &netHttpServerAttrsGetter{}

	tests := []struct {
		name       string
		url        string
		isTls      bool
		wantScheme string
	}{
		{"HTTP with scheme", "http://example.com/path", false, "http"},
		{"HTTPS with scheme", "https://example.com/path", true, "https"},
		{"No scheme HTTP", "/path", false, "http"},
		{"No scheme HTTPS", "/path", true, "https"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsedURL, _ := url.Parse(tt.url)
			req := &netHttpRequest{url: parsedURL, isTls: tt.isTls}
			scheme := getter.GetURLScheme(req)
			assert.Equal(t, tt.wantScheme, scheme)
		})
	}
}

func TestServerAttrsGetter_GetURLPath(t *testing.T) {
	getter := &netHttpServerAttrsGetter{}

	tests := []struct {
		name     string
		url      string
		wantPath string
	}{
		{"Simple path", "http://example.com/api/users", "/api/users"},
		{"Root path", "http://example.com/", "/"},
		{"Path with query", "http://example.com/search?q=test", "/search"},
		{"Empty path", "http://example.com", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsedURL, _ := url.Parse(tt.url)
			req := &netHttpRequest{url: parsedURL}
			path := getter.GetURLPath(req)
			assert.Equal(t, tt.wantPath, path)
		})
	}
}

func TestServerAttrsGetter_GetURLQuery(t *testing.T) {
	getter := &netHttpServerAttrsGetter{}

	tests := []struct {
		name      string
		url       string
		wantQuery string
	}{
		{"Single param", "http://example.com/?q=test", "q=test"},
		{"Multiple params", "http://example.com/?q=test&page=2", "q=test&page=2"},
		{"No query", "http://example.com/path", ""},
		{"Empty query", "http://example.com/?", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsedURL, _ := url.Parse(tt.url)
			req := &netHttpRequest{url: parsedURL}
			query := getter.GetURLQuery(req)
			assert.Equal(t, tt.wantQuery, query)
		})
	}
}

func TestServerAttrsGetter_GetNetworkType(t *testing.T) {
	getter := &netHttpServerAttrsGetter{}
	req := &netHttpRequest{}
	resp := &netHttpResponse{}

	// Currently returns "ipv4" (hardcoded in loongsuite)
	netType := getter.GetNetworkType(req, resp)
	assert.Equal(t, "ipv4", netType)
}

func TestServerAttrsGetter_GetNetworkTransport(t *testing.T) {
	getter := &netHttpServerAttrsGetter{}
	req := &netHttpRequest{}
	resp := &netHttpResponse{}

	transport := getter.GetNetworkTransport(req, resp)
	assert.Equal(t, "tcp", transport)
}

func TestServerAttrsGetter_GetNetworkProtocolName(t *testing.T) {
	getter := &netHttpServerAttrsGetter{}
	resp := &netHttpResponse{}

	tests := []struct {
		name         string
		isTls        bool
		wantProtocol string
	}{
		{"HTTP", false, "http"},
		{"HTTPS", true, "https"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &netHttpRequest{isTls: tt.isTls}
			protocol := getter.GetNetworkProtocolName(req, resp)
			assert.Equal(t, tt.wantProtocol, protocol)
		})
	}
}

func TestServerAttrsGetter_GetNetworkProtocolVersion(t *testing.T) {
	getter := &netHttpServerAttrsGetter{}
	resp := &netHttpResponse{}

	tests := []struct {
		name        string
		version     string
		wantVersion string
	}{
		{"HTTP/1.1", "1.1", "1.1"},
		{"HTTP/2", "2", "2"},
		{"HTTP/3", "3", "3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &netHttpRequest{version: tt.version}
			version := getter.GetNetworkProtocolVersion(req, resp)
			assert.Equal(t, tt.wantVersion, version)
		})
	}
}

func TestServerAttrsGetter_GetNetworkLocalInetAddress(t *testing.T) {
	getter := &netHttpServerAttrsGetter{}
	req := &netHttpRequest{}
	resp := &netHttpResponse{}

	// Currently returns empty string (not captured in HTTP requests)
	addr := getter.GetNetworkLocalInetAddress(req, resp)
	assert.Equal(t, "", addr)
}

func TestServerAttrsGetter_GetNetworkLocalPort(t *testing.T) {
	getter := &netHttpServerAttrsGetter{}
	req := &netHttpRequest{}
	resp := &netHttpResponse{}

	// Currently returns 0 (not captured in HTTP requests)
	port := getter.GetNetworkLocalPort(req, resp)
	assert.Equal(t, 0, port)
}

func TestServerAttrsGetter_GetNetworkPeerInetAddress(t *testing.T) {
	getter := &netHttpServerAttrsGetter{}
	resp := &netHttpResponse{}

	req := &netHttpRequest{host: "example.com"}
	addr := getter.GetNetworkPeerInetAddress(req, resp)
	assert.Equal(t, "example.com", addr)
}

func TestServerAttrsGetter_GetNetworkPeerPort(t *testing.T) {
	getter := &netHttpServerAttrsGetter{}
	resp := &netHttpResponse{}

	tests := []struct {
		name     string
		url      string
		wantPort int
	}{
		{"Explicit port", "http://example.com:8080/path", 8080},
		{"HTTPS default port", "https://example.com/path", 0},
		{"HTTP default port", "http://example.com/path", 0},
		{"Invalid port", "http://example.com:invalid/path", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsedURL, _ := url.Parse(tt.url)
			req := &netHttpRequest{url: parsedURL}
			port := getter.GetNetworkPeerPort(req, resp)
			assert.Equal(t, tt.wantPort, port)
		})
	}
}

func TestServerAttrsGetter_GetHTTPRoute(t *testing.T) {
	getter := &netHttpServerAttrsGetter{}

	tests := []struct {
		name      string
		url       string
		wantRoute string
	}{
		{"API route", "http://example.com/api/users", "/api/users"},
		{"Root route", "http://example.com/", "/"},
		{"Route with query", "http://example.com/search?q=test", "/search"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsedURL, _ := url.Parse(tt.url)
			req := &netHttpRequest{url: parsedURL}
			route := getter.GetHTTPRoute(req)
			assert.Equal(t, tt.wantRoute, route)
		})
	}
}
