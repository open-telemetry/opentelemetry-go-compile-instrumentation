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

func TestOpenAIClient(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "openaiclient", "go", "build", "-a")

	testCases := []struct {
		name         string
		model        string
		cachedTokens int64
	}{
		{
			name:  "chat_completion",
			model: "gpt-4",
		},
		{
			name:         "chat_completion_with_prompt_cache",
			model:        "gpt-4o-mini",
			cachedTokens: 8,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f := testutil.NewTestFixture(t)
			server := startMockOpenAIServer(t, tc.cachedTokens)

			f.Run("openaiclient",
				fmt.Sprintf("-addr=%s/v1", server.URL),
				"-api-key=test-key",
				fmt.Sprintf("-model=%s", tc.model),
			)

			span := f.RequireSingleSpan()
			testutil.RequireGenAIClientSemconv(
				t,
				span,
				"openai",                        // system
				"chat",                          // operationName
				tc.model,                        // requestModel
				"local",                         // providerName (localhost maps to "local")
				server.Listener.Addr().String(), // serverEndpoint
				"chatcmpl-test-123",             // responseID
				tc.model,                        // responseModel
				[]string{"stop"},                // finishReasons
				10,                              // inputTokens
				20,                              // outputTokens
				30,                              // totalTokens
			)

			// Cached prompt tokens appear only when the response reports a
			// cache hit. OpenAI already counts them inside prompt_tokens, so
			// inputTokens above stays at 10 rather than growing by the cached
			// count the way the Anthropic instrumentation's does.
			if tc.cachedTokens > 0 {
				testutil.RequireAttribute(t, span, "gen_ai.usage.cache_read.input_tokens", tc.cachedTokens)
			}
		})
	}
}

// TestOpenAIClientV2 exercises the openai-go/v2 instrumentation package end
// to end via otelc go build, rather than only unit-testing it in isolation.
// v1 and v3 are otherwise identical shares of the same internal/streaming
// module (a sibling of v2/v3 nested under v1's own directory in the repo,
// not a descendant of either), so this is the only integration coverage of
// the tool actually resolving that shared module for a consumer that never
// imports v1 at all.
func TestOpenAIClientV2(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "openaiclientv2", "go", "build", "-a")

	f := testutil.NewTestFixture(t)
	server := startMockOpenAIServer(t, 0)

	f.Run("openaiclientv2",
		fmt.Sprintf("-addr=%s/v1", server.URL),
		"-api-key=test-key",
		"-model=gpt-4",
	)

	span := f.RequireSingleSpan()
	testutil.RequireGenAIClientSemconv(
		t,
		span,
		"openai",                        // system
		"chat",                          // operationName
		"gpt-4",                         // requestModel
		"local",                         // providerName (localhost maps to "local")
		server.Listener.Addr().String(), // serverEndpoint
		"chatcmpl-test-123",             // responseID
		"gpt-4",                         // responseModel
		[]string{"stop"},                // finishReasons
		10,                              // inputTokens
		20,                              // outputTokens
		30,                              // totalTokens
	)
}

// TestOpenAIClientV3 exercises the openai-go/v3 instrumentation package end
// to end via otelc go build. This also makes the v3 module part of the
// instrumented-binary govulncheck coverage derived from test/apps.
func TestOpenAIClientV3(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "openaiclientv3", "go", "build", "-a")

	f := testutil.NewTestFixture(t)
	server := startMockOpenAIServer(t, 0)

	f.Run("openaiclientv3",
		fmt.Sprintf("-addr=%s/v1", server.URL),
		"-api-key=test-key",
		"-model=gpt-4",
	)

	span := f.RequireSingleSpan()
	testutil.RequireGenAIClientSemconv(
		t,
		span,
		"openai",
		"chat",
		"gpt-4",
		"local",
		server.Listener.Addr().String(),
		"chatcmpl-test-123",
		"gpt-4",
		[]string{"stop"},
		10,
		20,
		30,
	)
}

// startMockOpenAIServer creates a mock OpenAI API server for testing. A
// non-zero cachedTokens is reported under usage.prompt_tokens_details to
// exercise the prompt-cache attribute path.
func startMockOpenAIServer(t *testing.T, cachedTokens int64) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		// Parse model from request body
		var reqBody struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		usage := map[string]any{
			"prompt_tokens":     10,
			"completion_tokens": 20,
			"total_tokens":      30,
		}
		if cachedTokens > 0 {
			usage["prompt_tokens_details"] = map[string]any{"cached_tokens": cachedTokens}
		}

		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"id":     "chatcmpl-test-123",
			"object": "chat.completion",
			"model":  reqBody.Model,
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "Hello!",
					},
					"finish_reason": "stop",
				},
			},
			"usage": usage,
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}
