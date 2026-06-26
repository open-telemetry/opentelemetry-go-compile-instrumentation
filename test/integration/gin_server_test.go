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
	"go.opentelemetry.io/collector/pdata/ptrace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/testutil"
)

func TestGinServer(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "ginserver", "go", "build", "-a")

	testCases := []struct {
		name       string
		path       string
		wantStatus int
		assertSpan func(t *testing.T, span ptrace.Span)
	}{
		{
			name:       "matched route is enriched with http.route",
			path:       "/hello/OpenTelemetry",
			wantStatus: http.StatusOK,
			assertSpan: func(t *testing.T, span ptrace.Span) {
				// The single most important assertion: the span name must
				// use the route template, not the literal URL path. This is
				// the entire reason this package exists on top of net/http.
				assert.Equal(t, "GET /hello/:name", span.Name(),
					"span name must be route pattern, not literal URL")

				testutil.RequireAttribute(t, span, string(semconv.HTTPRouteKey), "/hello/:name")
				testutil.RequireAttribute(t, span, string(semconv.HTTPRequestMethodKey), "GET")
				testutil.RequireAttribute(t, span, string(semconv.HTTPResponseStatusCodeKey), int64(200))
				testutil.RequireAttribute(t, span, string(semconv.URLPathKey), "/hello/OpenTelemetry")
			},
		},
		{
			name:       "5xx response carries error.type",
			path:       fmt.Sprintf("/status/%d", http.StatusInternalServerError),
			wantStatus: http.StatusInternalServerError,
			assertSpan: func(t *testing.T, span ptrace.Span) {
				testutil.RequireAttribute(t, span, string(semconv.HTTPResponseStatusCodeKey), int64(500))
				testutil.RequireAttributeExists(t, span, string(semconv.ErrorTypeKey))
			},
		},
		{
			name:       "c.Error() surfaces as span status and exception event",
			path:       "/error",
			wantStatus: http.StatusOK,
			assertSpan: func(t *testing.T, span ptrace.Span) {
				assert.Equal(t, "GET /error", span.Name())
				assert.Equal(t, ptrace.StatusCodeError, span.Status().Code(),
					"span status must be Error when c.Error() was called")
				assert.GreaterOrEqual(t, span.Events().Len(), 1,
					"span must have at least one exception event from RecordError")
			},
		},
		{
			name:       "unmatched route keeps plain method as span name",
			path:       "/no-such-route",
			wantStatus: http.StatusNotFound,
			assertSpan: func(t *testing.T, span ptrace.Span) {
				// For unmatched paths gin does not populate c.FullPath() so
				// the hook bails out. The span name must remain the plain
				// method from the upstream net/http instrumentation and
				// http.route must not be set. This guards against a
				// cardinality regression where every probed URL would
				// otherwise turn into a unique span name.
				assert.Equal(t, "GET", span.Name(),
					"span name must remain plain method when no gin route matches")

				_, hasRoute := testutil.Attrs(span)[string(semconv.HTTPRouteKey)]
				assert.False(t, hasRoute,
					"http.route must not be set when no gin route matches")

				testutil.RequireAttribute(t, span, string(semconv.HTTPResponseStatusCodeKey), int64(404))
				testutil.RequireAttribute(t, span, string(semconv.URLPathKey), "/no-such-route")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f := testutil.NewTestFixture(t)
			port := testutil.FreePort(t)

			f.Start("ginserver", fmt.Sprintf("-port=%d", port))
			testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", port))

			resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d%s", port, tc.path)) //nolint:noctx
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			require.Equal(t, tc.wantStatus, resp.StatusCode)

			testutil.WaitForSpanFlush(t)

			f.RequireTraceCount(1)
			f.RequireSpansPerTrace(1)
			span := testutil.RequireSpan(t, f.Traces(), testutil.IsServer)
			tc.assertSpan(t, span)
		})
	}
}
