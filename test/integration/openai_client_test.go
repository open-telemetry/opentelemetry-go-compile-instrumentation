// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/testutil"
)

// TestOpenAIClient runs the same assertions against every supported major
// version of github.com/openai/openai-go. The per-version test apps live in
// test/apps/openaiclientv{1,2,3} and are instrumented via the shared HTTP
// middleware wired up by pkg/instrumentation/openai/v{1,2,3}.
func TestOpenAIClient(t *testing.T) {
	testCases := []struct {
		name    string
		appName string
	}{
		{name: "v1", appName: "openaiclientv1"},
		{name: "v2", appName: "openaiclientv2"},
		{name: "v3", appName: "openaiclientv3"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f := testutil.NewTestFixture(t)
			f.Build(tc.appName)

			// The OpenAI client will fail to connect (no mock server),
			// but the instrumentation should still create a span with an error status and expected attributes.
			f.Run(tc.appName)
			testutil.WaitForSpanFlush(t)

			spans := testutil.AllSpans(f.Traces())
			require.GreaterOrEqual(t, len(spans), 1, "expected at least 1 span (chat completion)")

			// Verify chat completion span
			chatSpan := testutil.RequireSpan(t, f.Traces(),
				testutil.IsClient,
				testutil.HasAttribute("gen_ai.system", "openai"),
				testutil.HasAttribute("gen_ai.operation.name", "chat"),
			)
			// Verify error status since connection is expected to fail
			require.Equal(t, ptrace.StatusCodeError, chatSpan.Status().Code(), "expected ERROR status for failed connection")
			testutil.RequireGenAIClientSemconv(
				t,
				chatSpan,
				"chat",
				"gpt-4",
			)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/v1/chat/completions", r.URL.Path)
				w.Header().Set("Content-Type", "text/event-stream")
				_, err := w.Write([]byte(strings.Join([]string{
					`data: {"id":"chatcmpl-stream","model":"gpt-4-0613","choices":[{"delta":{"content":"hi"}}]}`,
					"",
					`data: {"id":"chatcmpl-stream","model":"gpt-4-0613","choices":[{"finish_reason":"stop"}]}`,
					"",
					`data: {"id":"chatcmpl-stream","model":"gpt-4-0613","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":20}}`,
					"",
					"data: [DONE]",
					"",
				}, "\n")))
				require.NoError(t, err)
			}))
			t.Cleanup(server.Close)

			f.Run(tc.appName, "-base-url", server.URL+"/v1", "-stream")
			testutil.WaitForSpanFlush(t)

			streamingSpan := testutil.RequireSpan(t, f.Traces(),
				testutil.IsClient,
				testutil.HasAttribute("gen_ai.system", "openai"),
				testutil.HasAttribute("gen_ai.operation.name", "chat"),
				testutil.HasAttribute("gen_ai.response.id", "chatcmpl-stream"),
			)
			require.Equal(t, ptrace.StatusCodeUnset, streamingSpan.Status().Code(), "expected UNSET status for successful stream")
			testutil.RequireGenAIClientSemconv(
				t,
				streamingSpan,
				"chat",
				"gpt-4",
			)
			testutil.RequireAttribute(t, streamingSpan, "gen_ai.response.id", "chatcmpl-stream")
			testutil.RequireAttribute(t, streamingSpan, "gen_ai.response.model", "gpt-4-0613")
			testutil.RequireAttribute(t, streamingSpan, "gen_ai.usage.input_tokens", int64(10))
			testutil.RequireAttribute(t, streamingSpan, "gen_ai.usage.output_tokens", int64(20))
		})
	}
}
