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

	// Send request to Fiber server endpoint
	url := fmt.Sprintf("http://127.0.0.1:%d/hello/world", port)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Wait for span from the Fiber server to be flushed
	f.WaitForSpans(1)

	f.RequireTraceCount(1)
	f.RequireSpansPerTrace(1)

	fiberServerSpan := testutil.RequireSpan(t, f.Traces(), testutil.IsServer, func(s ptrace.Span) bool {
		return s.Name() == "GET /hello/:name"
	})

	require.False(t, fiberServerSpan.TraceID().IsEmpty(), "Fiber server span must have a valid TraceID")
}
