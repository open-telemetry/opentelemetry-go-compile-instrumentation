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

	"go.opentelemetry.io/otelc/test/testutil"
)

func TestEchoServer(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "echoserver", "go", "build", "-a")

	testCases := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		assertSpan func(t *testing.T, span ptrace.Span)
	}{
		{
			name:       "matched route is enriched with http.route",
			method:     http.MethodGet,
			path:       "/hello/OpenTelemetry",
			wantStatus: http.StatusOK,
			assertSpan: func(t *testing.T, span ptrace.Span) {
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
			method:     http.MethodGet,
			path:       fmt.Sprintf("/status/%d", http.StatusInternalServerError),
			wantStatus: http.StatusInternalServerError,
			assertSpan: func(t *testing.T, span ptrace.Span) {
				testutil.RequireAttribute(t, span, string(semconv.HTTPResponseStatusCodeKey), int64(500))
				testutil.RequireAttributeExists(t, span, string(semconv.ErrorTypeKey))
			},
		},
		{
			name:       "c.Error() surfaces as span status and exception event",
			method:     http.MethodGet,
			path:       "/error",
			wantStatus: http.StatusInternalServerError,
			assertSpan: func(t *testing.T, span ptrace.Span) {
				assert.Equal(t, "GET /error", span.Name())
				assert.Equal(t, ptrace.StatusCodeError, span.Status().Code(),
					"span status must be Error when c.Error() was called")
				assert.GreaterOrEqual(t, span.Events().Len(), 1,
					"span must have at least one exception event from RecordError")
			},
		},
		{
			name:       "handler return err surfaces as span status and exception event",
			method:     http.MethodGet,
			path:       "/returned-error",
			wantStatus: http.StatusInternalServerError,
			assertSpan: func(t *testing.T, span ptrace.Span) {
				assert.Equal(t, "GET /returned-error", span.Name())
				assert.Equal(t, ptrace.StatusCodeError, span.Status().Code(),
					"span status must be Error when the handler returned an error")
				assert.GreaterOrEqual(t, span.Events().Len(), 1,
					"span must have at least one exception event from RecordError")
			},
		},
		{
			name:       "method not allowed keeps plain method as span name",
			method:     http.MethodPost,
			path:       "/hello/OpenTelemetry",
			wantStatus: http.StatusMethodNotAllowed,
			assertSpan: func(t *testing.T, span ptrace.Span) {
				assert.Equal(t, "POST", span.Name(),
					"405 must not use the GET template as the span name")

				_, hasRoute := testutil.Attrs(span)[string(semconv.HTTPRouteKey)]
				assert.False(t, hasRoute, "http.route must not be set on 405")
				testutil.RequireAttribute(t, span, string(semconv.HTTPResponseStatusCodeKey), int64(405))
				assert.NotEqual(t, ptrace.StatusCodeError, span.Status().Code(),
					"405 must not set span status Error")
			},
		},
		{
			name:       "unmatched route keeps plain method as span name",
			method:     http.MethodGet,
			path:       "/no-such-route",
			wantStatus: http.StatusNotFound,
			assertSpan: func(t *testing.T, span ptrace.Span) {
				assert.Equal(t, "GET", span.Name(),
					"span name must remain plain method when no echo route matches")

				_, hasRoute := testutil.Attrs(span)[string(semconv.HTTPRouteKey)]
				assert.False(t, hasRoute,
					"http.route must not be set when no echo route matches")

				testutil.RequireAttribute(t, span, string(semconv.HTTPResponseStatusCodeKey), int64(404))
				testutil.RequireAttribute(t, span, string(semconv.URLPathKey), "/no-such-route")
				assert.NotEqual(t, ptrace.StatusCodeError, span.Status().Code(),
					"404 must not set span status Error")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f := testutil.NewTestFixture(t)
			port := testutil.FreePort(t)

			f.Start("echoserver", fmt.Sprintf("-port=%d", port))
			testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", port))

			req, err := http.NewRequestWithContext(
				t.Context(),
				tc.method,
				fmt.Sprintf("http://127.0.0.1:%d%s", port, tc.path),
				nil,
			)
			require.NoError(t, err)
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			require.Equal(t, tc.wantStatus, resp.StatusCode)

			f.WaitForSpans(1)

			f.RequireTraceCount(1)
			f.RequireSpansPerTrace(1)
			span := testutil.RequireSpan(t, f.Traces(), testutil.IsServer)
			tc.assertSpan(t, span)
		})
	}
}
