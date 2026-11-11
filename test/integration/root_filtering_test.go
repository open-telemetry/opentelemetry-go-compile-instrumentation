// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"go.opentelemetry.io/otelc/test/testutil"
)

func TestRootFiltering(t *testing.T) {
	t.Parallel()

	// Build both applications once to verify that runtime configuration works
	// without rebuilding the instrumented binary across all test scenarios.
	testutil.Build(t, "", "httpserverdbclient", "go", "build", "-a")
	testutil.Build(t, "", "dbclient", "go", "build", "-a", "-trimpath")

	t.Run("ExcludedServerPathsAndDescendantSuppression", func(t *testing.T) {
		f := testutil.NewTestFixture(t)
		f.SetEnv("OTEL_GO_EXCLUDED_SERVER_PATHS", "/healthz,/readyz")
		f.SetEnv("OTEL_GO_DROP_ROOT_CLIENT_PRODUCER", "true")

		port := testutil.FreePort(t)
		f.Start("httpserverdbclient", fmt.Sprintf("-front-port=%d", port))
		testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", port))

		// 1. Excluded health-check request: executes successfully (200 OK)
		// Its handler performs an internal DB query (SELECT 1). Both the root SERVER
		// span and its local descendant CLIENT span must be suppressed.
		healthURL := fmt.Sprintf("http://127.0.0.1:%d/healthz", port)
		healthReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, healthURL, nil)
		require.NoError(t, err)
		healthResp, err := http.DefaultClient.Do(healthReq)
		require.NoError(t, err)
		defer healthResp.Body.Close()
		require.Equal(t, http.StatusOK, healthResp.StatusCode)

		// 2. Ordinary application request: must produce normal trace with Server and SQL Client spans.
		helloURL := fmt.Sprintf("http://127.0.0.1:%d/hello", port)
		helloReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, helloURL, nil)
		require.NoError(t, err)
		helloResp, err := http.DefaultClient.Do(helloReq)
		require.NoError(t, err)
		defer helloResp.Body.Close()
		require.Equal(t, http.StatusOK, helloResp.StatusCode)

		// Wait for the spans from the ordinary request.
		f.WaitForSpans(2)

		// Exactly 1 trace with 2 spans must be collected: only /hello and its DB query.
		// If /healthz or its DB query were recorded, trace count or span count would be higher.
		f.RequireTraceCount(1)
		f.RequireSpansPerTrace(2)

		spans := testutil.AllSpans(f.Traces())
		require.Len(t, spans, 2)

		serverSpan := testutil.RequireSpan(
			t,
			f.Traces(),
			testutil.IsServer,
			func(s ptrace.Span) bool { return s.Name() == "GET /hello" },
		)
		clientSpan := testutil.RequireSpan(t, f.Traces(), testutil.IsClient)

		require.Equal(t, serverSpan.TraceID(), clientSpan.TraceID())
		require.Equal(t, serverSpan.SpanID(), clientSpan.ParentSpanID())
		require.True(t, serverSpan.ParentSpanID().IsEmpty())
	})

	t.Run("ParentlessClientSuppressed", func(t *testing.T) {
		f := testutil.NewTestFixture(t)
		f.SetEnv("OTEL_GO_DROP_ROOT_CLIENT_PRODUCER", "true")

		// Run parentless database ping to process completion.
		f.Run("dbclient", "-op=ping")

		spans := testutil.AllSpans(f.Traces())
		require.Empty(t, spans, "parentless client span must be dropped when filter is enabled")
	})

	t.Run("FilteringDisabledBehaviorUnchanged", func(t *testing.T) {
		// When filtering options are not set, both /healthz and parentless client spans are recorded.
		t.Run("ServerAndDescendantRecorded", func(t *testing.T) {
			f := testutil.NewTestFixture(t)

			port := testutil.FreePort(t)
			f.Start("httpserverdbclient", fmt.Sprintf("-front-port=%d", port))
			testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", port))

			healthURL := fmt.Sprintf("http://127.0.0.1:%d/healthz", port)
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, healthURL, nil)
			require.NoError(t, err)
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, http.StatusOK, resp.StatusCode)

			f.WaitForSpans(2)
			f.RequireTraceCount(1)
			f.RequireSpansPerTrace(2)

			serverSpan := testutil.RequireSpan(
				t,
				f.Traces(),
				testutil.IsServer,
				func(s ptrace.Span) bool { return s.Name() == "GET /healthz" },
			)
			clientSpan := testutil.RequireSpan(t, f.Traces(), testutil.IsClient)
			require.Equal(t, serverSpan.TraceID(), clientSpan.TraceID())
		})

		t.Run("ParentlessClientRecorded", func(t *testing.T) {
			f := testutil.NewTestFixture(t)

			f.Run("dbclient", "-op=ping")

			span := f.RequireSingleSpan()
			require.Equal(t, "PING", span.Name())
		})
	})
}
