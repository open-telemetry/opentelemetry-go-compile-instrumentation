// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otelc/test/testutil"
)

func TestAnthropicClient(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "anthropicclient", "go", "build", "-a")

	testCases := []struct {
		name  string
		model string
	}{
		{
			name:  "messages",
			model: "claude-sonnet-4-5",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f := testutil.NewTestFixture(t)
			server := startMockAnthropicServer(t)

			f.Run("anthropicclient",
				fmt.Sprintf("-addr=%s", server.URL),
				"-api-key=test-key",
				fmt.Sprintf("-model=%s", tc.model),
			)

			span := f.RequireSingleSpan()
			testutil.RequireGenAIClientSemconv(
				t,
				span,
				"anthropic",          // system
				"chat",               // operationName
				tc.model,             // requestModel
				"local",              // providerName (127.0.0.1 maps to "local")
				"msg-test-123",       // responseID
				tc.model,             // responseModel
				[]string{"end_turn"}, // finishReasons
				10,                   // inputTokens
				20,                   // outputTokens
				30,                   // totalTokens (computed input + output)
			)
		})
	}
}

// startMockAnthropicServer creates a mock Anthropic API server for testing.
func startMockAnthropicServer(t *testing.T) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		// Parse model from request body
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
