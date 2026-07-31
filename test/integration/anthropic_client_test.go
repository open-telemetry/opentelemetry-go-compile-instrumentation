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
		name          string
		model         string
		cacheRead     int64
		cacheCreation int64
	}{
		{
			name:  "messages",
			model: "claude-sonnet-4-5",
		},
		{
			name:          "messages_with_prompt_cache",
			model:         "claude-sonnet-4-5",
			cacheRead:     7,
			cacheCreation: 3,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f := testutil.NewTestFixture(t)
			server := startMockAnthropicServer(t, tc.cacheRead, tc.cacheCreation)

			f.Run("anthropicclient",
				fmt.Sprintf("-addr=%s", server.URL),
				"-api-key=test-key",
				fmt.Sprintf("-model=%s", tc.model),
			)

			// Anthropic's input_tokens excludes cache tokens; the
			// instrumentation folds them back in.
			inputTokens := int64(10) + tc.cacheRead + tc.cacheCreation
			outputTokens := int64(20)

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
				inputTokens,
				outputTokens,
				inputTokens+outputTokens, // totalTokens (computed)
			)

			// Anthropic-specific prompt-cache attributes appear only when the
			// response reports cache activity.
			if tc.cacheRead > 0 {
				testutil.RequireAttribute(t, span, "gen_ai.usage.cache_read.input_tokens", tc.cacheRead)
			}
			if tc.cacheCreation > 0 {
				testutil.RequireAttribute(t, span, "gen_ai.usage.cache_creation.input_tokens", tc.cacheCreation)
			}
		})
	}
}

// startMockAnthropicServer creates a mock Anthropic API server for testing.
// Non-zero cache token values are included in the response usage to exercise
// the prompt-cache attribute path.
func startMockAnthropicServer(t *testing.T, cacheRead, cacheCreation int64) *httptest.Server {
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

		usage := map[string]any{
			"input_tokens":  10,
			"output_tokens": 20,
		}
		if cacheRead > 0 {
			usage["cache_read_input_tokens"] = cacheRead
		}
		if cacheCreation > 0 {
			usage["cache_creation_input_tokens"] = cacheCreation
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
			"usage":         usage,
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}
