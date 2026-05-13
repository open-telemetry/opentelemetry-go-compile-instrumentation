// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/testutil"
)

func TestGinServer(t *testing.T) {
	f := testutil.NewTestFixture(t)

	f.BuildAndStart("ginserver")
	testutil.WaitForTCP(t, "127.0.0.1:8080")

	resp, err := http.Get("http://127.0.0.1:8080/hello/OpenTelemetry") //nolint:noctx
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusOK, resp.StatusCode)

	testutil.WaitForSpanFlush(t)

	f.RequireTraceCount(1)
	f.RequireSpansPerTrace(1)

	span := testutil.RequireSpan(t, f.Traces(), testutil.IsServer)

	// The single most important assertion: the span name must use the route
	// template, not the literal URL path. This is the entire reason this
	// package exists on top of the net/http instrumentation.
	assert.Equal(t, "GET /hello/:name", span.Name(),
		"span name must be route pattern, not literal URL")

	testutil.RequireAttribute(t, span, string(semconv.HTTPRouteKey), "/hello/:name")
	testutil.RequireAttribute(t, span, string(semconv.HTTPRequestMethodKey), "GET")
	testutil.RequireAttribute(t, span, string(semconv.HTTPResponseStatusCodeKey), int64(200))
	testutil.RequireAttribute(t, span, string(semconv.URLPathKey), "/hello/OpenTelemetry")
}

func TestGinServer_ServerError(t *testing.T) {
	f := testutil.NewTestFixture(t)

	f.BuildAndStart("ginserver")
	testutil.WaitForTCP(t, "127.0.0.1:8080")

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:8080/status/%d", http.StatusInternalServerError)) //nolint:noctx
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	testutil.WaitForSpanFlush(t)

	f.RequireTraceCount(1)
	span := testutil.RequireSpan(t, f.Traces(), testutil.IsServer)

	testutil.RequireAttribute(t, span, string(semconv.HTTPResponseStatusCodeKey), int64(500))
	testutil.RequireAttributeExists(t, span, string(semconv.ErrorTypeKey))
}
