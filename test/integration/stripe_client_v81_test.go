// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"go.opentelemetry.io/otelc/test/testutil"
)

// expectedStripeV81Spans is the set of spans mode=smoke must produce.
//
// v81 predates stripe.Client and the v2 API surface, so this app drives the
// client.API aggregate instead. A different client surface reaching the same
// single hook is exactly what this test exists to prove.
//
// Keep in sync with test/apps/stripeclientv81 main.go.
var expectedStripeV81Spans = []struct {
	spanName string
	method   string
	template string
	path     string
}{
	{"POST /v1/customers", "POST", "/v1/customers", "/v1/customers"},
	{"GET /v1/customers/{id}", "GET", "/v1/customers/{id}", "/v1/customers/" + stripeTestCustomerID},
	{"GET /v1/customers", "GET", "/v1/customers", "/v1/customers"},
}

func TestStripeClientV81(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "stripeclientv81", "go", "build", "-a")

	t.Run("smoke", func(t *testing.T) {
		f := testutil.NewTestFixture(t)
		server := startStripeMockAPI(t)

		output := f.Run("stripeclientv81", "-addr="+server.URL, "-mode=smoke", "-id="+stripeTestCustomerID)
		require.Contains(t, output, "created customer id="+stripeTestCustomerID)
		require.Contains(t, output, "customer id="+stripeTestCustomerID)
		require.Contains(t, output, "customers count=1")

		for _, want := range expectedStripeV81Spans {
			span := testutil.RequireSpan(t, f.Traces(),
				testutil.IsClient,
				testutil.HasName(want.spanName),
			)
			attrs := testutil.Attrs(span)
			require.Equal(t, ptrace.SpanKindClient, span.Kind())
			require.Equal(t, want.method, attrs["http.request.method"])
			require.Equal(t, want.template, attrs["url.template"])
			require.Equal(t, want.path, attrs["url.path"])
			require.Equal(t, int64(200), attrs["http.response.status_code"])
			require.Equal(t, "req_mock123", attrs["stripe.request_id"])
			require.NotEmpty(t, attrs["server.address"])
			// server.port is conditionally required and the mock runs on a
			// non-default port.
			require.NotZero(t, attrs["server.port"])
			require.Equal(t, ptrace.StatusCodeUnset, span.Status().Code())
		}
	})

	t.Run("not_found", func(t *testing.T) {
		f := testutil.NewTestFixture(t)
		server := startStripeMockAPI(t)

		_ = f.Run("stripeclientv81", "-addr="+server.URL, "-mode=not_found", "-id=cus_MissingXyz123")

		span := testutil.RequireSpan(t, f.Traces(),
			testutil.IsClient,
			testutil.HasName("GET /v1/customers/{id}"),
		)
		attrs := testutil.Attrs(span)
		require.Equal(t, int64(404), attrs["http.response.status_code"])
		require.Equal(t, "404", attrs["error.type"])
		require.Equal(t, "resource_missing", attrs["stripe.error.code"])
		require.Equal(t, "invalid_request_error", attrs["stripe.error.type"])
		require.Equal(t, ptrace.StatusCodeError, span.Status().Code())

		// The unknown customer ID must not leak into the span name.
		require.NotContains(t, span.Name(), "cus_MissingXyz123")
	})
}
