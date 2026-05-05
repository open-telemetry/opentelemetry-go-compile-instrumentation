// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

func TestWaitForSpans_returnsWhenSpansArrive(t *testing.T) {
	c := &Collector{traces: ptrace.NewTraces()}

	rs := c.traces.ResourceSpans().AppendEmpty()
	ss := rs.ScopeSpans().AppendEmpty()
	ss.Spans().AppendEmpty()

	start := time.Now()
	WaitForSpans(t, c, 1)
	assert.Less(t, time.Since(start), defaultSpanFlushTimeout)
}

func TestWaitForSpans_pollsUntilSpansArrive(t *testing.T) {
	c := &Collector{traces: ptrace.NewTraces()}

	go func() {
		time.Sleep(100 * time.Millisecond)
		c.mu.Lock()
		rs := c.traces.ResourceSpans().AppendEmpty()
		ss := rs.ScopeSpans().AppendEmpty()
		ss.Spans().AppendEmpty()
		c.mu.Unlock()
	}()

	WaitForSpans(t, c, 1)
	assert.Equal(t, 1, len(AllSpans(c.GetTraces())))
}
