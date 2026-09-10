// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"bytes"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

// sendSpan posts a single span to the collector via its HTTP endpoint.
func sendSpan(t *testing.T, c *Collector) {
	t.Helper()
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	ss := rs.ScopeSpans().AppendEmpty()
	ss.Spans().AppendEmpty()

	var m ptrace.ProtoMarshaler
	data, err := m.MarshalTraces(td)
	require.NoError(t, err)

	resp, err := http.Post(c.URL+"/v1/traces", "application/x-protobuf", bytes.NewReader(data))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestWaitForSpans_returnsWhenSpansArrive(t *testing.T) {
	c := StartCollector(t)
	sendSpan(t, c)

	start := time.Now()
	WaitForSpans(t, c, 1)
	assert.Less(t, time.Since(start), 100*time.Millisecond)
}

func TestWaitForSpans_pollsUntilSpansArrive(t *testing.T) {
	c := StartCollector(t)

	go func() {
		time.Sleep(100 * time.Millisecond)
		sendSpan(t, c)
	}()

	WaitForSpans(t, c, 1)
	assert.Equal(t, 1, c.SpanCount())
}

func TestWaitForSpans_timesOut(t *testing.T) {
	c := StartCollector(t)
	// No spans sent — polling should report failure within the short timeout.
	ok := pollForSpans(c, 1, 50*time.Millisecond)
	assert.False(t, ok)
}

func TestFreePort(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		port := FreePort(t)
		assert.Greater(t, port, 0)
	})

	const numGoroutines = 50
	ports := make(chan int, numGoroutines)

	t.Run("concurrent", func(t *testing.T) {
		for i := 0; i < numGoroutines; i++ {
			t.Run(fmt.Sprintf("goroutine-%d", i), func(t *testing.T) {
				t.Parallel()
				ports <- FreePort(t)
			})
		}
	})

	close(ports)
	allocated := make(map[int]bool)
	for port := range ports {
		assert.False(t, allocated[port], "duplicate port returned: %d", port)
		allocated[port] = true
	}
}
