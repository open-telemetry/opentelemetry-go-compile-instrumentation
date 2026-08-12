// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package netutil

import (
	"net/url"
	"testing"
)

func TestHTTPServerEndpoint(t *testing.T) {
	tests := []struct {
		name        string
		rawURL      string
		wantAddress string
		wantPort    int
	}{
		{
			name:        "HTTP default port",
			rawURL:      "http://api.openai.com/v1/chat/completions",
			wantAddress: "api.openai.com",
			wantPort:    80,
		},
		{
			name:        "HTTPS default port",
			rawURL:      "https://api.anthropic.com/v1/messages",
			wantAddress: "api.anthropic.com",
			wantPort:    443,
		},
		{
			name:        "explicit default port",
			rawURL:      "https://api.openai.com:443/v1/chat/completions",
			wantAddress: "api.openai.com",
			wantPort:    443,
		},
		{
			name:        "custom port",
			rawURL:      "https://proxy.example.com:8443/v1/messages",
			wantAddress: "proxy.example.com",
			wantPort:    8443,
		},
		{
			name:        "IPv6 with custom port",
			rawURL:      "http://[2001:db8::1]:8080/v1/chat/completions",
			wantAddress: "2001:db8::1",
			wantPort:    8080,
		},
		{
			name:        "unsupported scheme",
			rawURL:      "custom://example.com/v1/messages",
			wantAddress: "example.com",
		},
		{
			name:   "relative URL",
			rawURL: "/v1/messages",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := url.Parse(tt.rawURL)
			if err != nil {
				t.Fatalf("parse URL: %v", err)
			}

			address, port := HTTPServerEndpoint(u)
			if address != tt.wantAddress {
				t.Errorf("address = %q, want %q", address, tt.wantAddress)
			}
			if port != tt.wantPort {
				t.Errorf("port = %d, want %d", port, tt.wantPort)
			}
		})
	}
}

func TestHTTPServerEndpointNilURL(t *testing.T) {
	address, port := HTTPServerEndpoint(nil)
	if address != "" || port != 0 {
		t.Errorf("HTTPServerEndpoint(nil) = (%q, %d), want (\"\", 0)", address, port)
	}
}
