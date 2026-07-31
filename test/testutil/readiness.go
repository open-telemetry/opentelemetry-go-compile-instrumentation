// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	defaultReadinessTimeout  = 10 * time.Second
	defaultReadinessInterval = 100 * time.Millisecond
	defaultSpanPollTimeout  = 3 * time.Second
	defaultSpanPollInterval  = 25 * time.Millisecond
)

// WaitForTCP waits until a TCP connection can be established.
func WaitForTCP(t *testing.T, addr string) {
	t.Helper()
	require.Eventuallyf(t, func() bool {
		conn, err := net.DialTimeout("tcp", addr, defaultReadinessInterval)
		if err == nil {
			conn.Close()
			return true
		}
		return false
	}, defaultReadinessTimeout, defaultReadinessInterval, "timeout waiting for TCP readiness at %s", addr)
}

// WaitForSpans polls the collector until at least minSpans spans are received or the timeout expires.
func WaitForSpans(t *testing.T, c *Collector, minSpans int) {
	t.Helper()
	if !pollForSpans(c, minSpans, defaultSpanPollTimeout) {
		t.Fatalf("timeout waiting for %d span(s), collector has %d", minSpans, c.SpanCount())
	}
}

// pollForSpans returns true if at least minSpans spans arrive within timeout.
func pollForSpans(c *Collector, minSpans int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if c.SpanCount() >= minSpans {
			return true
		}
		time.Sleep(defaultSpanPollInterval)
	}
	return false
}

// FreePort returns a port the OS just assigned for "localhost:0". The
// listener is closed before returning, so the test app can bind to it.
// There is a tiny race window between close and rebind; acceptable for CI.
func FreePort(t *testing.T) int {
	t.Helper()
	lis, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	port := lis.Addr().(*net.TCPAddr).Port
	require.NoError(t, lis.Close())
	return port
}
