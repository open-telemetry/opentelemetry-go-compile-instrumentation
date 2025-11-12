// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package nethttp

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetProtocolVersion(t *testing.T) {
	tests := []struct {
		name          string
		majorVersion  int
		minorVersion  int
		expectedProto string
	}{
		{
			name:          "HTTP/1.0",
			majorVersion:  1,
			minorVersion:  0,
			expectedProto: "1.0",
		},
		{
			name:          "HTTP/1.1",
			majorVersion:  1,
			minorVersion:  1,
			expectedProto: "1.1",
		},
		{
			name:          "HTTP/1.2",
			majorVersion:  1,
			minorVersion:  2,
			expectedProto: "1.2",
		},
		{
			name:          "HTTP/2",
			majorVersion:  2,
			minorVersion:  0,
			expectedProto: "2",
		},
		{
			name:          "HTTP/3",
			majorVersion:  3,
			minorVersion:  0,
			expectedProto: "3",
		},
		{
			name:          "Unknown version",
			majorVersion:  4,
			minorVersion:  5,
			expectedProto: "4.5",
		},
		{
			name:          "Zero version",
			majorVersion:  0,
			minorVersion:  0,
			expectedProto: "0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getProtocolVersion(tt.majorVersion, tt.minorVersion)
			assert.Equal(t, tt.expectedProto, result)
		})
	}
}

func TestNetHttpRequestStruct(t *testing.T) {
	testURL, err := url.Parse("https://example.com/path?query=value")
	assert.NoError(t, err)

	header := http.Header{}
	header.Set("Content-Type", "application/json")
	header.Set("User-Agent", "test-agent")

	req := &netHttpRequest{
		method:  "POST",
		url:     testURL,
		host:    "example.com",
		isTls:   true,
		header:  header,
		version: "1.1",
	}

	assert.Equal(t, "POST", req.method)
	assert.Equal(t, testURL, req.url)
	assert.Equal(t, "example.com", req.host)
	assert.True(t, req.isTls)
	assert.Equal(t, "application/json", req.header.Get("Content-Type"))
	assert.Equal(t, "test-agent", req.header.Get("User-Agent"))
	assert.Equal(t, "1.1", req.version)
}

func TestNetHttpResponseStruct(t *testing.T) {
	header := http.Header{}
	header.Set("Content-Type", "application/json")
	header.Set("X-Custom-Header", "custom-value")

	resp := &netHttpResponse{
		statusCode: 200,
		header:     header,
	}

	assert.Equal(t, 200, resp.statusCode)
	assert.Equal(t, "application/json", resp.header.Get("Content-Type"))
	assert.Equal(t, "custom-value", resp.header.Get("X-Custom-Header"))
}

func TestNetHttpResponseStructWithDifferentStatusCodes(t *testing.T) {
	testCases := []struct {
		name       string
		statusCode int
	}{
		{"OK", 200},
		{"Created", 201},
		{"Bad Request", 400},
		{"Not Found", 404},
		{"Internal Server Error", 500},
		{"Service Unavailable", 503},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resp := &netHttpResponse{
				statusCode: tc.statusCode,
				header:     http.Header{},
			}
			assert.Equal(t, tc.statusCode, resp.statusCode)
		})
	}
}
