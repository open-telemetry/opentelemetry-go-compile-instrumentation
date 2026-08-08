// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	defaultReadinessTimeout  = 10 * time.Second
	defaultReadinessInterval = 100 * time.Millisecond
	defaultSpanPollTimeout   = 3 * time.Second
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

var (
	allocatedPortsMu sync.Mutex
	allocatedPorts   = make(map[int]time.Time)
)

// FreePort returns a port the OS just assigned for "localhost:0". The
// listener is closed before returning, so the test app can bind to it.
// There is a tiny race window between close and rebind; defended against
// concurrent allocations by keeping a registry of recently returned ports.
func FreePort(t *testing.T) int {
	t.Helper()

	allocatedPortsMu.Lock()
	now := time.Now()
	for p, allocatedAt := range allocatedPorts {
		if now.Sub(allocatedAt) > 30*time.Second {
			delete(allocatedPorts, p)
		}
	}
	allocatedPortsMu.Unlock()

	const maxAttempts = 100
	for i := 0; i < maxAttempts; i++ {
		select {
		case <-t.Context().Done():
			t.Fatalf("FreePort: context cancelled: %v", t.Context().Err())
		default:
		}

		lis, err := net.Listen("tcp", "localhost:0")
		if err != nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		port := lis.Addr().(*net.TCPAddr).Port
		require.NoError(t, lis.Close())

		allocatedPortsMu.Lock()
		if _, exists := allocatedPorts[port]; !exists {
			allocatedPorts[port] = time.Now()
			allocatedPortsMu.Unlock()
			return port
		}
		allocatedPortsMu.Unlock()

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("FreePort: failed to allocate a unique free port after %d attempts", maxAttempts)
	return 0
}
