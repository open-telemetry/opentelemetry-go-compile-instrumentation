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
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

func TestExplicitInstrumentationSelection(t *testing.T) {
	t.Parallel()

	testutil.Build(t, "", "gincustom", "go", "build", "-a")

	// Verify .otelc-build/matched.json only contains net/http, custom gin instrumentation
	// but not the built-in gin instrumentation:
	matched := filepath.Join("../", "apps", "gincustom", ".otelc-build", "matched.json")
	require.FileExists(t, matched)

	matchedData, readErr := os.ReadFile(matched)
	require.NoError(t, readErr)
	require.Contains(
		t,
		string(matchedData),
		util.OtelcRoot+"/instrumentation/net/http/server",
	)
	require.Contains(
		t,
		string(matchedData),
		util.OtelcRoot+"/test/apps/gincustom/instrumentation",
	)
	require.NotContains(
		t,
		string(matchedData),
		util.OtelcRoot+"/instrumentation/github.com/gin-gonic/gin",
	)

	f := testutil.NewTestFixture(t)
	port := testutil.FreePort(t)

	f.Start("gincustom", fmt.Sprintf("-port=%d", port))
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

	// The app is instrumented with a custom gin instrumentation
	// instead of otelc's default gin instrumentation, so the span
	// name must remain the plain HTTP method name ("GET") and
	// http.route attribute must not be set
	assert.Equal(t, "GET", span.Name(),
		"span name must remain the plain HTTP method")

	_, hasRoute := testutil.Attrs(span)[string(semconv.HTTPRouteKey)]
	assert.False(t, hasRoute,
		"http.route must not be set when gin instrumentation is not enabled")

	testutil.RequireAttribute(t, span, string(semconv.HTTPRequestMethodKey), "GET")
	testutil.RequireAttribute(t, span, string(semconv.HTTPResponseStatusCodeKey), int64(http.StatusOK))
	testutil.RequireAttribute(t, span, string(semconv.URLPathKey), "/hello/OpenTelemetry")

	// We also want to ensure our custom gin attribute is set correctly
	testutil.RequireAttribute(t, span, "gin.otelc.custom", true)
}
