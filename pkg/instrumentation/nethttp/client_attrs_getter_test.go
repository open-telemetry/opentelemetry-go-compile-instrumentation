// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package nethttp

import (
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClientAttrsGetter_GetRequestMethod(t *testing.T) {
	getter := &netHttpClientAttrsGetter{}
	req := &netHttpRequest{method: "GET"}
	assert.Equal(t, "GET", getter.GetRequestMethod(req))
}

func TestClientAttrsGetter_GetHTTPRequestHeader(t *testing.T) {
	getter := &netHttpClientAttrsGetter{}
	header := http.Header{}
	header.Set("Authorization", "Bearer token")
	header.Add("Accept", "application/json")
	header.Add("Accept", "text/html")

	req := &netHttpRequest{header: header}

	auth := getter.GetHTTPRequestHeader(req, "Authorization")
	assert.Equal(t, []string{"Bearer token"}, auth)

	accept := getter.GetHTTPRequestHeader(req, "Accept")
	assert.Equal(t, []string{"application/json", "text/html"}, accept)
}

func TestClientAttrsGetter_GetHTTPResponseStatusCode(t *testing.T) {
	getter := &netHttpClientAttrsGetter{}
	req := &netHttpRequest{}

	tests := []struct {
		name       string
		resp       *netHttpResponse
		err        error
		wantStatus int
	}{
		{"OK", &netHttpResponse{statusCode: 200}, nil, 200},
		{"Created", &netHttpResponse{statusCode: 201}, nil, 201},
		{"BadRequest", &netHttpResponse{statusCode: 400}, nil, 400},
		{"ServerError", &netHttpResponse{statusCode: 500}, nil, 500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := getter.GetHTTPResponseStatusCode(req, tt.resp, tt.err)
			assert.Equal(t, tt.wantStatus, status)
		})
	}
}

func TestClientAttrsGetter_GetHTTPResponseHeader(t *testing.T) {
	getter := &netHttpClientAttrsGetter{}
	req := &netHttpRequest{}
	header := http.Header{}
	header.Set("Content-Type", "application/json")
	header.Add("Cache-Control", "no-cache")

	resp := &netHttpResponse{header: header}

	contentType := getter.GetHTTPResponseHeader(req, resp, "Content-Type")
	assert.Equal(t, []string{"application/json"}, contentType)

	cache := getter.GetHTTPResponseHeader(req, resp, "Cache-Control")
	assert.Equal(t, []string{"no-cache"}, cache)
}

func TestClientAttrsGetter_GetErrorType(t *testing.T) {
	getter := &netHttpClientAttrsGetter{}
	req := &netHttpRequest{}
	resp := &netHttpResponse{statusCode: 500}

	tests := []struct {
		name          string
		err           error
		wantErrorType string
	}{
		{
			name:          "No error",
			err:           nil,
			wantErrorType: "",
		},
		{
			name:          "Connection error",
			err:           errors.New("connection refused"),
			wantErrorType: "connection refused",
		},
		{
			name:          "Timeout error",
			err:           errors.New("timeout"),
			wantErrorType: "timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errorType := getter.GetErrorType(req, resp, tt.err)
			assert.Equal(t, tt.wantErrorType, errorType)
		})
	}
}

func TestClientAttrsGetter_GetNetworkType(t *testing.T) {
	getter := &netHttpClientAttrsGetter{}
	resp := &netHttpResponse{}

	tests := []struct {
		name            string
		host            string
		wantNetworkType string
	}{
		{
			name:            "IPv4 address",
			host:            "192.168.1.1",
			wantNetworkType: "ipv4",
		},
		{
			name:            "IPv4 with port",
			host:            "192.168.1.1:8080",
			wantNetworkType: "ipv4",
		},
		{
			name:            "IPv6 address",
			host:            "2001:0db8:85a3:0000:0000:8a2e:0370:7334",
			wantNetworkType: "ipv6",
		},
		{
			name:            "IPv6 with brackets",
			host:            "[2001:db8::1]",
			wantNetworkType: "ipv6",
		},
		{
			name:            "IPv6 with brackets and port",
			host:            "[2001:db8::1]:8080",
			wantNetworkType: "ipv6",
		},
		{
			name:            "IPv6 loopback",
			host:            "::1",
			wantNetworkType: "ipv6",
		},
		{
			name:            "Hostname",
			host:            "api.example.com",
			wantNetworkType: "ipv4",
		},
		{
			name:            "Hostname with port",
			host:            "api.example.com:8080",
			wantNetworkType: "ipv4",
		},
		{
			name:            "Empty host",
			host:            "",
			wantNetworkType: "ipv4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &netHttpRequest{host: tt.host}
			netType := getter.GetNetworkType(req, resp)
			assert.Equal(t, tt.wantNetworkType, netType)
		})
	}
}

func TestClientAttrsGetter_GetNetworkTransport(t *testing.T) {
	getter := &netHttpClientAttrsGetter{}
	req := &netHttpRequest{}
	resp := &netHttpResponse{}

	transport := getter.GetNetworkTransport(req, resp)
	assert.Equal(t, "tcp", transport)
}

func TestClientAttrsGetter_GetNetworkProtocolName(t *testing.T) {
	getter := &netHttpClientAttrsGetter{}
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

func TestClientAttrsGetter_GetNetworkProtocolVersion(t *testing.T) {
	getter := &netHttpClientAttrsGetter{}
	resp := &netHttpResponse{}

	req := &netHttpRequest{version: "1.1"}
	version := getter.GetNetworkProtocolVersion(req, resp)
	assert.Equal(t, "1.1", version)
}

func TestClientAttrsGetter_GetNetworkPeerInetAddress(t *testing.T) {
	getter := &netHttpClientAttrsGetter{}
	resp := &netHttpResponse{}

	req := &netHttpRequest{host: "api.example.com"}
	addr := getter.GetNetworkPeerInetAddress(req, resp)
	assert.Equal(t, "api.example.com", addr)
}

func TestClientAttrsGetter_GetNetworkPeerPort(t *testing.T) {
	getter := &netHttpClientAttrsGetter{}
	resp := &netHttpResponse{}

	tests := []struct {
		name     string
		url      string
		wantPort int
	}{
		{"Explicit port", "https://api.example.com:8443/path", 8443},
		{"HTTPS default", "https://api.example.com/path", 0},
		{"HTTP default", "http://api.example.com/path", 0},
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

func TestClientAttrsGetter_GetURLFull(t *testing.T) {
	getter := &netHttpClientAttrsGetter{}

	tests := []struct {
		name    string
		url     string
		wantURL string
	}{
		{"Full URL", "https://api.example.com:8443/path?query=value", "https://api.example.com:8443/path?query=value"},
		{"Simple URL", "http://localhost/api", "http://localhost/api"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsedURL, _ := url.Parse(tt.url)
			req := &netHttpRequest{url: parsedURL}
			fullURL := getter.GetURLFull(req)
			assert.Equal(t, tt.wantURL, fullURL)
		})
	}
}

func TestClientAttrsGetter_GetServerAddress(t *testing.T) {
	getter := &netHttpClientAttrsGetter{}

	req := &netHttpRequest{host: "api.example.com"}
	addr := getter.GetServerAddress(req)
	assert.Equal(t, "api.example.com", addr)
}

func TestClientAttrsGetter_GetServerPort(t *testing.T) {
	getter := &netHttpClientAttrsGetter{}

	tests := []struct {
		name     string
		url      string
		wantPort int
	}{
		{"Explicit port", "https://api.example.com:8443/path", 8443},
		{"No port", "https://api.example.com/path", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsedURL, _ := url.Parse(tt.url)
			req := &netHttpRequest{url: parsedURL}
			port := getter.GetServerPort(req)
			assert.Equal(t, tt.wantPort, port)
		})
	}
}
