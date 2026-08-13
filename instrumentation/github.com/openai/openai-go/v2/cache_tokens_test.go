// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package v2

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

const cacheReadTokensKey = "gen_ai.usage.cache_read.input_tokens"

// chatSpanAttrsForUsage runs one non-streaming chat call through the middleware
// with the given usage JSON and returns the recorded span attributes.
func chatSpanAttrsForUsage(t *testing.T, usage string) []attribute.KeyValue {
	t.Helper()
	sr := setupTestTracer(t)

	req, _ := http.NewRequest(
		http.MethodPost,
		"http://api.openai.com/v1/chat/completions",
		io.NopCloser(bytes.NewReader([]byte(`{"model":"gpt-4o-mini"}`))),
	)
	respBody := `{"id":"chatcmpl-1","model":"gpt-4o-mini","choices":[{"finish_reason":"stop"}],"usage":` + usage + `}`
	next := func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader([]byte(respBody))),
		}, nil
	}

	_, err := OtelMiddleware()(req, next)
	require.NoError(t, err)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	return spans[0].Attributes()
}

// TestChatRecordsCacheReadTokens verifies the cached prompt tokens OpenAI
// reports under usage.prompt_tokens_details are recorded, and - the important
// part - that they are not added to gen_ai.usage.input_tokens. OpenAI already
// counts cached tokens inside prompt_tokens, so folding them in the way the
// Anthropic instrumentation does would double-count the cached portion.
func TestChatRecordsCacheReadTokens(t *testing.T) {
	attrs := chatSpanAttrsForUsage(t,
		`{"prompt_tokens":2048,"completion_tokens":64,"total_tokens":2112,`+
			`"prompt_tokens_details":{"cached_tokens":1024}}`)

	assertInt64Attribute(t, attrs, cacheReadTokensKey, 1024)
	assertInt64Attribute(t, attrs, "gen_ai.usage.input_tokens", 2048)
	assertInt64Attribute(t, attrs, "gen_ai.usage.total_tokens", 2112)
}

// TestChatOmitsCacheReadTokensWhenUnused verifies the attribute is absent
// rather than recorded as zero when the request did not hit the cache, matching
// the anthropic-sdk-go instrumentation.
func TestChatOmitsCacheReadTokensWhenUnused(t *testing.T) {
	tests := []struct {
		name  string
		usage string
	}{
		{
			name:  "details absent entirely",
			usage: `{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}`,
		},
		{
			name: "details present but no cache hit",
			usage: `{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30,` +
				`"prompt_tokens_details":{"cached_tokens":0}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := chatSpanAttrsForUsage(t, tt.usage)

			assertNoAttribute(t, attrs, cacheReadTokensKey)
			assertInt64Attribute(t, attrs, "gen_ai.usage.input_tokens", 10)
		})
	}
}
