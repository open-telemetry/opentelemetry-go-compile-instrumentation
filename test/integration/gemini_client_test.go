// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"go.opentelemetry.io/otelc/test/testutil"
)

func TestGeminiClient(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "geminiclient", "go", "build", "-a")

	testCases := []struct {
		name           string
		model          string
		thoughtsTokens int64
		// wantOutputTokens is candidatesTokenCount plus any thinking tokens,
		// which Gemini reports separately but bills and generates as output.
		wantOutputTokens int64
		wantTotalTokens  int64
	}{
		{
			name:             "generate_content",
			model:            "gemini-2.5-flash",
			wantOutputTokens: 20,
			wantTotalTokens:  30,
		},
		{
			name:             "generate_content_with_thinking",
			model:            "gemini-2.5-pro",
			thoughtsTokens:   7,
			wantOutputTokens: 27,
			wantTotalTokens:  37,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f := testutil.NewTestFixture(t)
			server := startMockGeminiServer(t, tc.thoughtsTokens)

			f.Run("geminiclient",
				fmt.Sprintf("-addr=%s", server.URL),
				"-api-key=test-key",
				fmt.Sprintf("-model=%s", tc.model),
			)

			span := f.RequireSingleSpan()

			// Span name follows the GenAI convention:
			// "{gen_ai.operation.name} {gen_ai.request.model}".
			require.Equal(t, "chat "+tc.model, span.Name())
			require.Equal(t, ptrace.SpanKindClient, span.Kind())

			testutil.RequireGenAIClientSemconv(
				t,
				span,
				"gemini",            // system
				"chat",              // operationName
				tc.model,            // requestModel
				"gcp.gemini",        // providerName (Gemini Developer API backend)
				"resp-test-123",     // responseID
				tc.model+"-001",     // responseModel (modelVersion)
				[]string{"STOP"},    // finishReasons
				10,                  // inputTokens (promptTokenCount)
				tc.wantOutputTokens, // outputTokens (candidates + thoughts)
				tc.wantTotalTokens,  // totalTokens
			)

			// Generation parameters come from the request body, which the
			// instrumentation replays via Request.GetBody rather than consuming.
			testutil.RequireAttribute(t, span, "gen_ai.request.max_tokens", int64(64))
		})
	}
}

// startMockGeminiServer creates a mock Gemini generateContent endpoint. A
// non-zero thoughtsTokens value exercises the thinking-token path, where the
// model's reasoning tokens are reported apart from candidatesTokenCount.
func startMockGeminiServer(t *testing.T, thoughtsTokens int64) *httptest.Server {
	t.Helper()

	handler := func(w http.ResponseWriter, r *http.Request) {
		// Both backends address the model as ".../<model>:generateContent".
		if !strings.HasSuffix(r.URL.Path, ":generateContent") {
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
			return
		}

		segment := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		model, _, _ := strings.Cut(segment, ":")

		// The instrumentation must leave the request body intact.
		var reqBody struct {
			GenerationConfig struct {
				MaxOutputTokens int64 `json:"maxOutputTokens"`
			} `json:"generationConfig"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if reqBody.GenerationConfig.MaxOutputTokens != 64 {
			t.Errorf("server saw maxOutputTokens=%d, want 64 (request body was altered)",
				reqBody.GenerationConfig.MaxOutputTokens)
		}

		usage := map[string]any{
			"promptTokenCount":     10,
			"candidatesTokenCount": 20,
			"totalTokenCount":      30,
		}
		if thoughtsTokens > 0 {
			usage["thoughtsTokenCount"] = thoughtsTokens
			usage["totalTokenCount"] = 30 + thoughtsTokens
		}

		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"role":  "model",
						"parts": []map[string]any{{"text": "Hello!"}},
					},
					"finishReason": "STOP",
				},
			},
			"modelVersion":  model + "-001",
			"responseId":    "resp-test-123",
			"usageMetadata": usage,
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}

	server := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(server.Close)

	return server
}
