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

// WaitForSpanFlush waits for spans to be flushed to collector.
func WaitForSpanFlush(t *testing.T, c *Collector) {
	t.Helper()
	require.Eventually(t, func() bool {
		return c != nil && len(AllSpans(c.GetTraces())) > 0
	}, defaultReadinessTimeout, defaultReadinessInterval, "timeout waiting for spans to be flushed")
}
