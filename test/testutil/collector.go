// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"go.opentelemetry.io/collector/pdata/ptrace"
)

// Collector represents an in-memory OTLP collector for testing
type Collector struct {
	*httptest.Server
	mu     sync.Mutex
	traces ptrace.Traces
}

// GetTraces returns the collected traces with proper synchronization.
func (c *Collector) GetTraces() ptrace.Traces {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.traces
}

// StartCollector starts an in-memory OTLP HTTP server that collects traces
func StartCollector(t *testing.T) *Collector {
	c := &Collector{traces: ptrace.NewTraces()}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/traces", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("Failed to read request body: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		defer r.Body.Close()

		var unmarshaler ptrace.ProtoUnmarshaler
		traces, err := unmarshaler.UnmarshalTraces(body)
		if err != nil {
			t.Errorf("Failed to unmarshal OTLP traces: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		c.mu.Lock()
		traces.ResourceSpans().MoveAndAppendTo(c.traces.ResourceSpans())
		c.mu.Unlock()

		w.WriteHeader(http.StatusOK)
	})

	c.Server = httptest.NewServer(mux)
	t.Cleanup(c.Close)

	return c
}
