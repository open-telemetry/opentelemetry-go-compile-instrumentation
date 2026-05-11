// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"net"
	"testing"
	"time"
)

const (
	defaultReadinessTimeout  = 10 * time.Second
	defaultReadinessInterval = 100 * time.Millisecond
	defaultSpanFlushTimeout  = 3 * time.Second
	defaultSpanPollInterval  = 25 * time.Millisecond
)

// WaitForTCP waits until a TCP connection can be established.
func WaitForTCP(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(defaultReadinessTimeout)

	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, defaultReadinessInterval)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(defaultReadinessInterval)
	}
	t.Fatalf("timeout waiting for TCP readiness at %s", addr)
}

// WaitForSpans polls the collector until at least minSpans spans are received or the timeout expires.
func WaitForSpans(t *testing.T, c *Collector, minSpans int) {
	t.Helper()
	deadline := time.Now().Add(defaultSpanFlushTimeout)
	for time.Now().Before(deadline) {
		if c.SpanCount() >= minSpans {
			return
		}
		time.Sleep(defaultSpanPollInterval)
	}
	t.Fatalf("timeout waiting for %d span(s), collector has %d", minSpans, c.SpanCount())
}
