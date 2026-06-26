// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSplitHostPort(t *testing.T) {
	tests := []struct {
		name         string
		hostport     string
		expectedHost string
		expectedPort int
	}{
		{
			name:         "IPv4 with port",
			hostport:     "192.168.1.1:8080",
			expectedHost: "192.168.1.1",
			expectedPort: 8080,
		},
		{
			name:         "IPv6 with port",
			hostport:     "[::1]:8080",
			expectedHost: "::1",
			expectedPort: 8080,
		},
		{
			name:         "IPv6 full with port",
			hostport:     "[2001:db8::1]:50051",
			expectedHost: "2001:db8::1",
			expectedPort: 50051,
		},
		{
			name:         "hostname with port",
			hostport:     "localhost:50051",
			expectedHost: "localhost",
			expectedPort: 50051,
		},
		{
			name:         "hostname without port",
			hostport:     "localhost",
			expectedHost: "localhost",
			expectedPort: -1,
		},
		{
			name:         "IPv6 without port",
			hostport:     "[::1]",
			expectedHost: "::1",
			expectedPort: -1,
		},
		{
			name:         "invalid IPv6",
			hostport:     "[::1",
			expectedHost: "",
			expectedPort: -1,
		},
		{
			name:         "empty",
			hostport:     "",
			expectedHost: "",
			expectedPort: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, port := splitHostPort(tt.hostport)
			assert.Equal(t, tt.expectedHost, host)
			assert.Equal(t, tt.expectedPort, port)
		})
	}
}
