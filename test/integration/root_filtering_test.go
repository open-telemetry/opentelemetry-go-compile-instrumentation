// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"go.opentelemetry.io/otelc/test/testutil"
)

func TestRootFiltering(t *testing.T) {
	// Built sequentially once to verify that runtime configuration works
	// without rebuilding the instrumented binary across all test scenarios.
	testutil.Build(t, "", "httpserverdbclient", "go", "build", "-a")
	testutil.Build(t, "", "dbclient", "go", "build", "-a", "-trimpath")

	t.Run("ExcludedServerPath_SuppressesRootAndDescendant", func(t *testing.T) {
		f := testutil.NewTestFixture(t)
		f.SetEnv("OTEL_GO_EXCLUDED_SERVER_PATHS", "/healthz,/readyz")
		f.SetEnv("OTEL_GO_DROP_ROOT_CLIENT_PRODUCER", "false")
		f.SetEnv("OTEL_TRACES_SAMPLER", "always_on")

		port := testutil.FreePort(t)
		server := f.Start("httpserverdbclient", fmt.Sprintf("-front-port=%d", port), "-stdin-shutdown")
		testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", port))

		healthURL := fmt.Sprintf("http://127.0.0.1:%d/healthz", port)
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, healthURL, nil)
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, "ok", string(body), "database query in /healthz must execute successfully")

		// Graceful shutdown waits for in-flight requests and their instrumentation after-hooks
		// to complete. Under the configured SimpleSpanProcessor, completed spans are synchronously
		// exported before the server shuts down.
		server.Stop()

		require.Empty(t, testutil.AllSpans(f.Traces()),
			"excluded /healthz root SERVER span and descendant DB CLIENT span must not be exported")
	})

	t.Run("OrdinaryServerPath_ExportsServerAndDescendant", func(t *testing.T) {
		f := testutil.NewTestFixture(t)
		f.SetEnv("OTEL_GO_EXCLUDED_SERVER_PATHS", "/healthz,/readyz")
		f.SetEnv("OTEL_GO_DROP_ROOT_CLIENT_PRODUCER", "false")

		port := testutil.FreePort(t)
		server := f.Start("httpserverdbclient", fmt.Sprintf("-front-port=%d", port), "-stdin-shutdown")
		testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", port))

		helloURL := fmt.Sprintf("http://127.0.0.1:%d/hello", port)
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, helloURL, nil)
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		f.WaitForSpans(2)
		server.Stop()

		f.RequireTraceCount(1)
		f.RequireSpansPerTrace(2)

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

	t.Run("MixedRequests_OnlyOrdinaryPathExported", func(t *testing.T) {
		f := testutil.NewTestFixture(t)
		f.SetEnv("OTEL_GO_EXCLUDED_SERVER_PATHS", "/healthz,/readyz")
		f.SetEnv("OTEL_GO_DROP_ROOT_CLIENT_PRODUCER", "false")

		port := testutil.FreePort(t)
		server := f.Start("httpserverdbclient", fmt.Sprintf("-front-port=%d", port), "-stdin-shutdown")
		testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", port))

		// 1. Excluded request
		healthURL := fmt.Sprintf("http://127.0.0.1:%d/healthz", port)
		hReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, healthURL, nil)
		require.NoError(t, err)
		hResp, err := http.DefaultClient.Do(hReq)
		require.NoError(t, err)
		defer hResp.Body.Close()
		require.Equal(t, http.StatusOK, hResp.StatusCode)

		// 2. Ordinary request
		helloURL := fmt.Sprintf("http://127.0.0.1:%d/hello", port)
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, helloURL, nil)
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		f.WaitForSpans(2)
		server.Stop()

		f.RequireTraceCount(1)
		f.RequireSpansPerTrace(2)
		spans := testutil.AllSpans(f.Traces())
		require.Len(t, spans, 2)
	})

	t.Run("ParentlessClientSuppressed", func(t *testing.T) {
		f := testutil.NewTestFixture(t)
		f.SetEnv("OTEL_GO_DROP_ROOT_CLIENT_PRODUCER", "true")
		f.SetEnv("OTEL_GO_EXCLUDED_SERVER_PATHS", "")

		// Run parentless database ping to process completion.
		f.Run("dbclient", "-op=ping")

		spans := testutil.AllSpans(f.Traces())
		require.Empty(t, spans, "parentless client span must be dropped when filter is enabled")
	})

	t.Run("FilteringDisabledBehaviorUnchanged", func(t *testing.T) {
		t.Run("ServerAndDescendantRecorded", func(t *testing.T) {
			f := testutil.NewTestFixture(t)

			port := testutil.FreePort(t)
			server := f.Start("httpserverdbclient", fmt.Sprintf("-front-port=%d", port), "-stdin-shutdown")
			testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", port))

			healthURL := fmt.Sprintf("http://127.0.0.1:%d/healthz", port)
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, healthURL, nil)
			require.NoError(t, err)
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, http.StatusOK, resp.StatusCode)

			f.WaitForSpans(2)
			server.Stop()

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
