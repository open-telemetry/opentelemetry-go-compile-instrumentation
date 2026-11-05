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

func TestFiberServer(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "fiberserver", "go", "build", "-a")

	f := testutil.NewTestFixture(t)
	port := testutil.FreePort(t)

	f.Start("fiberserver", fmt.Sprintf("-port=%d", port))
	testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", port))

	get := func(t *testing.T, path string) int {
		t.Helper()
		url := fmt.Sprintf("http://127.0.0.1:%d%s", port, path)
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		return resp.StatusCode
	}

	t.Run("ok", func(t *testing.T) {
		require.Equal(t, http.StatusOK, get(t, "/hello/world"))
		f.WaitForSpans(1)

		span := testutil.RequireSpan(t, f.Traces(), testutil.IsServer, testutil.HasName("GET /hello/:name"))
		require.False(t, span.TraceID().IsEmpty(), "Fiber server span must have a valid TraceID")
		testutil.RequireAttribute(t, span, "http.response.status_code", int64(http.StatusOK))
		require.NotEqual(t, ptrace.StatusCodeError, span.Status().Code())
	})

	// The handler returns an error, so the status comes from AfterNext reading
	// the returned error, not from the response.
	t.Run("handler error is 500", func(t *testing.T) {
		require.Equal(t, http.StatusInternalServerError, get(t, "/fail"))
		f.WaitForSpans(2)

		span := testutil.RequireSpan(t, f.Traces(), testutil.IsServer, testutil.HasName("GET /fail"))
		testutil.RequireAttribute(t, span, "http.response.status_code", int64(http.StatusInternalServerError))
		require.Equal(t, ptrace.StatusCodeError, span.Status().Code())
	})

	f.RequireTraceCount(2)
	f.RequireSpansPerTrace(1)
}
