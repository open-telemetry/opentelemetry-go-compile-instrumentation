// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package v2

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"go.opentelemetry.io/otelc/instrumentation/github.com/openai/openai-go/v2/semconv"
)

// setupTestMeter wires the package-level tokenUsage histogram to a manual
// reader so tests can assert what was recorded.
func setupTestMeter(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	var err error
	tokenUsage, err = mp.Meter("test").Int64Histogram(
		"gen_ai.client.token.usage", metric.WithUnit("{token}"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = mp.Shutdown(context.Background())
		tokenUsage = nil
	})
	return reader
}

// tokenUsageByType collects gen_ai.client.token.usage and returns the summed
// value per gen_ai.token.type, e.g. {"input": 10, "output": 20}.
func tokenUsageByType(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))

	out := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "gen_ai.client.token.usage" {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[int64])
			require.True(t, ok, "gen_ai.client.token.usage must be an int64 histogram")
			for _, dp := range hist.DataPoints {
				v, found := dp.Attributes.Value(semconv.GenAITokenTypeKey)
				require.True(t, found, "data point missing gen_ai.token.type")
				out[v.AsString()] = dp.Sum
			}
		}
	}
	return out
}

// TestOtelMiddleware_RecordsTokenUsage_Chat verifies a chat call records one
// input and one output token.usage measurement, tagged by gen_ai.token.type.
func TestOtelMiddleware_RecordsTokenUsage_Chat(t *testing.T) {
	setupTestTracer(t)
	reader := setupTestMeter(t)

	req, _ := http.NewRequest(
		http.MethodPost,
		"http://api.openai.com/v1/chat/completions",
		io.NopCloser(bytes.NewReader([]byte(`{"model":"gpt-4"}`))),
	)
	next := func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(bytes.NewReader([]byte(
				`{"id":"chatcmpl-1","model":"gpt-4","choices":[{"finish_reason":"stop"}],` +
					`"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`))),
		}, nil
	}

	_, err := OtelMiddleware()(req, next)
	require.NoError(t, err)

	usage := tokenUsageByType(t, reader)
	assert.Equal(t, int64(10), usage["input"], "input tokens")
	assert.Equal(t, int64(20), usage["output"], "output tokens")
	assert.Len(t, usage, 2, "chat records both input and output")
}

// TestOtelMiddleware_RecordsTokenUsage_Embedding verifies embeddings record
// only input tokens (there are no output tokens to report).
func TestOtelMiddleware_RecordsTokenUsage_Embedding(t *testing.T) {
	setupTestTracer(t)
	reader := setupTestMeter(t)

	req, _ := http.NewRequest(
		http.MethodPost,
		"http://api.openai.com/v1/embeddings",
		io.NopCloser(bytes.NewReader([]byte(`{"model":"text-embedding-ada-002","input":"hello"}`))),
	)
	next := func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(bytes.NewReader([]byte(
				`{"model":"text-embedding-ada-002","usage":{"prompt_tokens":2,"total_tokens":2}}`))),
		}, nil
	}

	_, err := OtelMiddleware()(req, next)
	require.NoError(t, err)

	usage := tokenUsageByType(t, reader)
	assert.Equal(t, int64(2), usage["input"], "input tokens")
	assert.Len(t, usage, 1, "embeddings record only input tokens")
}

// TestOtelMiddleware_RecordsTokenUsage_Completion covers the text-completion
// operation, which runs through the same recordTokenUsage call as chat.
func TestOtelMiddleware_RecordsTokenUsage_Completion(t *testing.T) {
	setupTestTracer(t)
	reader := setupTestMeter(t)

	req, _ := http.NewRequest(
		http.MethodPost,
		"http://api.openai.com/v1/completions",
		io.NopCloser(bytes.NewReader([]byte(`{"model":"gpt-3.5-turbo-instruct"}`))),
	)
	next := func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(bytes.NewReader([]byte(
				`{"id":"cmpl-1","model":"gpt-3.5-turbo-instruct","choices":[{"finish_reason":"length"}],` +
					`"usage":{"prompt_tokens":4,"completion_tokens":10,"total_tokens":14}}`))),
		}, nil
	}

	_, err := OtelMiddleware()(req, next)
	require.NoError(t, err)

	usage := tokenUsageByType(t, reader)
	assert.Equal(t, int64(4), usage["input"], "input tokens")
	assert.Equal(t, int64(10), usage["output"], "output tokens")
	assert.Len(t, usage, 2, "text completion records both input and output")
}

// TestOtelMiddleware_TokenUsage_ZeroVersusAbsent pins the distinction between a
// usage object that reports genuine zeros, which is recorded, and a response
// with no usage object at all, which records nothing. Without this the data
// point count could not be used to tell the two cases apart.
func TestOtelMiddleware_TokenUsage_ZeroVersusAbsent(t *testing.T) {
	tests := []struct {
		name          string
		respBody      string
		wantDataPoint bool
	}{
		{
			name: "usage reports zeros",
			respBody: `{"id":"chatcmpl-1","model":"gpt-4","choices":[{"finish_reason":"stop"}],` +
				`"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`,
			wantDataPoint: true,
		},
		{
			name:          "no usage object at all",
			respBody:      `{"id":"chatcmpl-1","model":"gpt-4","choices":[{"finish_reason":"stop"}]}`,
			wantDataPoint: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupTestTracer(t)
			reader := setupTestMeter(t)

			req, _ := http.NewRequest(
				http.MethodPost,
				"http://api.openai.com/v1/chat/completions",
				io.NopCloser(bytes.NewReader([]byte(`{"model":"gpt-4"}`))),
			)
			next := func(_ *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(bytes.NewReader([]byte(tt.respBody))),
				}, nil
			}

			_, err := OtelMiddleware()(req, next)
			require.NoError(t, err)

			usage := tokenUsageByType(t, reader)
			if tt.wantDataPoint {
				assert.Len(t, usage, 2, "a reported zero must still produce a data point")
				assert.Equal(t, int64(0), usage["input"])
				assert.Equal(t, int64(0), usage["output"])
			} else {
				assert.Empty(t, usage, "absent usage must produce no data point")
			}
		})
	}
}
