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

// WaitForSpanFlush waits for spans to be flushed to collector.
func WaitForSpanFlush(t *testing.T) {
	t.Helper()
	time.Sleep(200 * time.Millisecond)
}
