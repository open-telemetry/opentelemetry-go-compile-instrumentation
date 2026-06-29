// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/testutil"
)

func TestExplicitInstrumentationSelection(t *testing.T) {
	t.Parallel()

	// Verify .otelc-build/matched.json only contains net/http but not gin instrumentation:
	matched := filepath.Join("../", "apps", "ginnethttp", ".otelc-build", "matched.json")
	require.FileExists(t, matched)

	matchedData, readErr := os.ReadFile(matched)
	require.NoError(t, readErr)
	require.Contains(
		t,
		string(matchedData),
		"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/nethttp/server",
	)
	require.NotContains(
		t,
		string(matchedData),
		"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/gin/server",
	)

	f := testutil.NewTestFixture(t)
	port := testutil.FreePort(t)

	f.Start("ginnethttp", fmt.Sprintf("-port=%d", port))
	testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", port))

	resp, getErr := http.Get(
		fmt.Sprintf("http://127.0.0.1:%d/hello/OpenTelemetry", port),
	) //nolint:noctx
	require.NoError(t, getErr)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusOK, resp.StatusCode)

	testutil.WaitForSpanFlush(t)

	f.RequireTraceCount(1)
	f.RequireSpansPerTrace(1)

	span := testutil.RequireSpan(t, f.Traces(), testutil.IsServer)

	// The app itself is implemented using gin, but only the net/http
	// instrumentation is enabled via otel.instrumentation.go. The gin
	// instrumentation must therefore not enrich the span with route
	// metadata or replace the plain span name.
	assert.Equal(t, "GET", span.Name(),
		"span name must remain the plain HTTP method")

	_, hasRoute := testutil.Attrs(span)[string(semconv.HTTPRouteKey)]
	assert.False(t, hasRoute,
		"http.route must not be set when gin instrumentation is not enabled")

	testutil.RequireAttribute(t, span, string(semconv.HTTPRequestMethodKey), "GET")
	testutil.RequireAttribute(t, span, string(semconv.HTTPResponseStatusCodeKey), int64(http.StatusOK))
	testutil.RequireAttribute(t, span, string(semconv.URLPathKey), "/hello/OpenTelemetry")
}
