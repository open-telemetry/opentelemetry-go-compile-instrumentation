// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"net"
	"strconv"
	"strings"
)

// splitHostPort splits a network address hostport of the form "host:port" into host and port.
// Returns host and port (or -1 if port not found/invalid).
func splitHostPort(hostport string) (host string, port int) {
	port = -1

	if strings.HasPrefix(hostport, "[") {
		// IPv6 address
		addrEnd := strings.LastIndexByte(hostport, ']')
		if addrEnd < 0 {
			// Invalid hostport
			return "", port
		}
		if i := strings.LastIndexByte(hostport[addrEnd:], ':'); i < 0 {
			host = hostport[1:addrEnd]
			return host, port
		}
	} else {
		if i := strings.LastIndexByte(hostport, ':'); i < 0 {
			host = hostport
			return host, port
		}
	}

	var pStr string
	var err error
	host, pStr, err = net.SplitHostPort(hostport)
	if err != nil {
		return "", port
	}

	p, err := strconv.ParseUint(pStr, 10, 16)
	if err != nil {
		return host, port
	}
	return host, int(p)
}
