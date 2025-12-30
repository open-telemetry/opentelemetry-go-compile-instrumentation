// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/collector/pdata/ptrace"
)

// Collector represents an in-memory OTLP collector for testing
type Collector struct {
	*httptest.Server
	Traces ptrace.Traces
}

// StartCollector starts an in-memory OTLP HTTP server that collects traces
func StartCollector(t *testing.T) *Collector {
	c := &Collector{Traces: ptrace.NewTraces()}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/traces", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("Failed to read request body: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		defer r.Body.Close()

		// Unmarshal OTLP protobuf traces
		var unmarshaler ptrace.ProtoUnmarshaler
		traces, err := unmarshaler.UnmarshalTraces(body)
		if err != nil {
			t.Errorf("Failed to unmarshal OTLP traces: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Append to collected traces
		traces.ResourceSpans().MoveAndAppendTo(c.Traces.ResourceSpans())

		w.WriteHeader(http.StatusOK)
	})

	c.Server = httptest.NewServer(mux)
	t.Cleanup(c.Close)

	return c
}
