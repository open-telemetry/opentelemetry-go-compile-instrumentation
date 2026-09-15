//go:build e2e

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"

	"go.opentelemetry.io/otelc/test/testutil"
)

func TestHTTPServerAnthropicClient(t *testing.T) {
	server := startMockAnthropicServer(t)

	f := testutil.NewTestFixture(t)

	frontPort := testutil.FreePort(t)
	addr := fmt.Sprintf("http://127.0.0.1:%d", frontPort)

	f.BuildAndStart(
		"httpserveranthropicclient",
		fmt.Sprintf("-front-port=%d", frontPort),
		fmt.Sprintf("-addr=%s", server.URL),
	)
	testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", frontPort))

	f.BuildAndRun("httpclient", "-addr", addr, "-name", "test")

	// BuildAndRun returns once the client exits, but the server span ends after
	// the response is written, so it may still be in flight. Wait for all three.
	f.WaitForSpans(3)

	// One distributed trace with three spans:
	// HTTP client -> HTTP server -> Anthropic client
	f.RequireTraceCount(1)
	f.RequireSpansPerTrace(3)

	httpClientSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsClient,
		testutil.HasAttributeContaining(string(semconv.URLFullKey), "/hello"),
	)
	httpServerSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsServer,
		testutil.HasAttribute(string(semconv.URLPathKey), "/hello"),
		testutil.HasAttribute(string(semconv.HTTPResponseStatusCodeKey), int64(200)),
	)
	genAISpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsClient,
		testutil.HasAttribute("gen_ai.system", "anthropic"),
	)

	require.Equal(t, httpClientSpan.TraceID(), httpServerSpan.TraceID(), "HTTP client and server must share a trace ID")
	require.Equal(
		t,
		httpServerSpan.TraceID(),
		genAISpan.TraceID(),
		"HTTP server and GenAI client must share a trace ID",
	)
	require.Equal(t, httpClientSpan.SpanID(), httpServerSpan.ParentSpanID(), "HTTP server parent must be HTTP client")
	require.Equal(t, httpServerSpan.SpanID(), genAISpan.ParentSpanID(), "GenAI client parent must be HTTP server")
	require.True(t, httpClientSpan.ParentSpanID().IsEmpty(), "HTTP client span must be the trace root")
}

func startMockAnthropicServer(t *testing.T) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		var reqBody struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"id":    "msg-test-123",
			"type":  "message",
			"role":  "assistant",
			"model": reqBody.Model,
			"content": []map[string]any{
				{
					"type": "text",
					"text": "Hello!",
				},
			},
			"stop_reason":   "end_turn",
			"stop_sequence": nil,
			"usage": map[string]any{
				"input_tokens":  10,
				"output_tokens": 20,
			},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}
