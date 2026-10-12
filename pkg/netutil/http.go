// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package netutil provides shared network parsing helpers.
package netutil

import (
	"net/url"
	"strconv"
	"strings"
)

const (
	defaultHTTPPort  = 80
	defaultHTTPSPort = 443
)

// HTTPServerEndpoint returns the server address and effective port in u.
// It infers the default port when an HTTP or HTTPS URL omits it.
func HTTPServerEndpoint(u *url.URL) (string, int) {
	if u == nil {
		return "", 0
	}

	address := u.Hostname()
	if address == "" {
		return "", 0
	}

	if portText := u.Port(); portText != "" {
		port, err := strconv.ParseUint(portText, 10, 16)
		if err != nil || port == 0 {
			return address, 0
		}
		return address, int(port)
	}

	switch strings.ToLower(u.Scheme) {
	case "http":
		return address, defaultHTTPPort
	case "https":
		return address, defaultHTTPSPort
	default:
		return address, 0
	}
}
