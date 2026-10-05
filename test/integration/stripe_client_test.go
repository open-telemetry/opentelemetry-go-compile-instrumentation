// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"go.opentelemetry.io/otelc/test/testutil"
)

const stripeTestCustomerID = "cus_NffrFeUfNV2Hib"

// expectedStripeSpans is the set of spans mode=smoke must produce. Every entry
// goes through the same single hook on the backend, which is the point: v1 and
// v2, GET and POST, two different backend instances.
//
// Keep in sync with test/apps/stripeclient main.go.
var expectedStripeSpans = []struct {
	spanName string
	method   string
	template string
	path     string
}{
	{"POST /v1/customers", "POST", "/v1/customers", "/v1/customers"},
	{"GET /v1/customers/{id}", "GET", "/v1/customers/{id}", "/v1/customers/" + stripeTestCustomerID},
	{"GET /v1/customers", "GET", "/v1/customers", "/v1/customers"},
	{
		"POST /v2/billing/meter_events", "POST",
		"/v2/billing/meter_events", "/v2/billing/meter_events",
	},
}

func TestStripeClient(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "stripeclient", "go", "build", "-a")

	t.Run("smoke", func(t *testing.T) {
		f := testutil.NewTestFixture(t)
		server := startStripeMockAPI(t)

		output := f.Run("stripeclient", "-addr="+server.URL, "-mode=smoke", "-id="+stripeTestCustomerID)
		require.Contains(t, output, "created customer id="+stripeTestCustomerID)
		require.Contains(t, output, "customer id="+stripeTestCustomerID)
		require.Contains(t, output, "customers count=1")
		require.Contains(t, output, "meter event identifier=mev_mock123")

		for _, want := range expectedStripeSpans {
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

		_ = f.Run("stripeclient", "-addr="+server.URL, "-mode=not_found", "-id=cus_MissingXyz123")

		span := testutil.RequireSpan(t, f.Traces(),
			testutil.IsClient,
			testutil.HasName("GET /v1/customers/{id}"),
		)
		attrs := testutil.Attrs(span)
		// The backend returns a nil response with the error, so these come from
		// the Stripe error body rather than the HTTP response.
		require.Equal(t, int64(404), attrs["http.response.status_code"])
		require.Equal(t, "404", attrs["error.type"])
		require.Equal(t, "resource_missing", attrs["stripe.error.code"])
		require.Equal(t, "invalid_request_error", attrs["stripe.error.type"])
		require.Equal(t, "req_mock123", attrs["stripe.request_id"])
		require.Equal(t, ptrace.StatusCodeError, span.Status().Code())

		// The unknown customer ID must not leak into the span name.
		require.NotContains(t, span.Name(), "cus_MissingXyz123")
	})
}

// startStripeMockAPI serves the minimal Stripe API responses the smoke and
// not_found modes need.
func startStripeMockAPI(t *testing.T) *httptest.Server {
	t.Helper()

	writeJSON := func(w http.ResponseWriter, status int, body any) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Request-Id", "req_mock123")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}

	customer := func(id string) map[string]any {
		return map[string]any{
			"id":      id,
			"object":  "customer",
			"email":   "test@example.com",
			"created": 1700000000,
		}
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch path := r.URL.Path; {
		case path == "/v1/customers" && r.Method == http.MethodPost:
			writeJSON(w, http.StatusOK, customer(stripeTestCustomerID))

		case path == "/v1/customers" && r.Method == http.MethodGet:
			writeJSON(w, http.StatusOK, map[string]any{
				"object":   "list",
				"url":      "/v1/customers",
				"has_more": false,
				"data":     []map[string]any{customer(stripeTestCustomerID)},
			})

		case strings.HasPrefix(path, "/v1/customers/"):
			id := strings.TrimPrefix(path, "/v1/customers/")
			if id != stripeTestCustomerID {
				writeJSON(w, http.StatusNotFound, map[string]any{"error": map[string]any{
					"type":    "invalid_request_error",
					"code":    "resource_missing",
					"message": "No such customer: " + id,
					"param":   "id",
				}})
				return
			}
			writeJSON(w, http.StatusOK, customer(id))

		case path == "/v2/billing/meter_events":
			writeJSON(w, http.StatusOK, map[string]any{
				"object":     "v2.billing.meter_event",
				"identifier": "mev_mock123",
				"event_name": "otelc_test_event",
				"created":    "2024-01-01T00:00:00.000Z",
				"livemode":   false,
				"payload":    map[string]string{"value": "1"},
			})

		default:
			writeJSON(w, http.StatusNotFound, map[string]any{"error": map[string]any{
				"type":    "invalid_request_error",
				"message": "no route " + path,
			}})
		}
	}))
	t.Cleanup(server.Close)
	return server
}
