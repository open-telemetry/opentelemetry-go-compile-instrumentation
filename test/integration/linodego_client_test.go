// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"go.opentelemetry.io/otelc/test/testutil"
)

// expectedSmokeOps is the set of public Client methods exercised by mode=smoke.
// Keep in sync with test/apps/linodegoclient main.go.
var expectedSmokeOps = []struct {
	operation string
	httpName  string // doRequest span name
}{
	{"ListRegions", "GET regions"},
	{"GetAccount", "GET account"},
	{"ListVolumes", "GET volumes"},
	{"GetVolume", "GET volumes/123"},
	{"ListInstances", "GET linode/instances"},
	{"GetInstance", "GET linode/instances/123"},
}

func TestLinodegoClient(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "linodegoclient", "go", "build", "-a")

	t.Run("smoke", func(t *testing.T) {
		f := testutil.NewTestFixture(t)
		server := startLinodeMockAPI(t)

		output := f.Run("linodegoclient", "-addr="+server.URL, "-mode=smoke", "-id=123")
		require.Contains(t, output, "regions count=")
		require.Contains(t, output, "account email=")
		require.Contains(t, output, "volume id=123")
		require.Contains(t, output, "instance id=123 label=test-instance")

		// Every public API op + its HTTP request span must appear.
		for _, op := range expectedSmokeOps {
			opSpan := testutil.RequireSpan(t, f.Traces(),
				testutil.IsClient,
				testutil.HasName("linodego."+op.operation),
			)
			requireLinodegoOperationSemconv(t, opSpan, op.operation)

			reqSpan := testutil.RequireSpan(t, f.Traces(),
				testutil.IsClient,
				testutil.HasName(op.httpName),
			)
			// Request spans use METHOD + relative path; split for method check.
			parts := strings.SplitN(op.httpName, " ", 2)
			require.Len(t, parts, 2)
			path := parts[1]
			if !strings.HasPrefix(path, "/") {
				path = "/" + path
			}
			requireLinodegoRequestSemconv(t, reqSpan, parts[0], path)
		}

		// Sanity: more than a single GetInstance pair of spans.
		spans := testutil.AllSpans(f.Traces())
		require.GreaterOrEqual(t, len(spans), len(expectedSmokeOps)*2,
			"expected at least one operation + request span per smoke op, got %d spans", len(spans))
	})

	t.Run("not_found", func(t *testing.T) {
		f := testutil.NewTestFixture(t)
		server := startLinodeMockAPI(t)

		_ = f.Run("linodegoclient", "-addr="+server.URL, "-mode=not_found", "-id=999")

		opSpan := testutil.RequireSpan(t, f.Traces(),
			testutil.IsClient,
			testutil.HasName("linodego.GetInstance"),
		)
		requireLinodegoOperationSemconv(t, opSpan, "GetInstance")
		attrs := testutil.Attrs(opSpan)
		require.Equal(t, int64(404), attrs["http.response.status_code"])
		require.Equal(t, "404", attrs["error.type"])
		require.Equal(t, ptrace.StatusCodeError, opSpan.Status().Code())

		reqSpan := testutil.RequireSpan(t, f.Traces(),
			testutil.IsClient,
			testutil.HasName("GET linode/instances/999"),
		)
		reqAttrs := testutil.Attrs(reqSpan)
		require.Equal(t, int64(404), reqAttrs["http.response.status_code"])
		require.Equal(t, ptrace.StatusCodeError, reqSpan.Status().Code())
	})
}

func requireLinodegoOperationSemconv(t *testing.T, span ptrace.Span, operation string) {
	t.Helper()
	require.Equal(t, ptrace.SpanKindClient, span.Kind())
	attrs := testutil.Attrs(span)
	require.Equal(t, operation, attrs["code.function.name"])
	require.Equal(t, "api.linode.com", attrs["server.address"])
}

func requireLinodegoRequestSemconv(t *testing.T, span ptrace.Span, method, path string) {
	t.Helper()
	require.Equal(t, ptrace.SpanKindClient, span.Kind())
	attrs := testutil.Attrs(span)
	require.Equal(t, method, attrs["http.request.method"])
	require.Equal(t, path, attrs["url.path"])
	require.Equal(t, "api.linode.com", attrs["server.address"])
}

// startLinodeMockAPI serves minimal Linode API JSON for the smoke-suite paths.
func startLinodeMockAPI(t *testing.T) *httptest.Server {
	t.Helper()

	writeJSON := func(w http.ResponseWriter, status int, body any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if err := json.NewEncoder(w).Encode(body); err != nil {
			fmt.Fprintf(w, `{"errors":[{"reason":"encode failed"}]}`)
		}
	}

	paginated := func(data any) map[string]any {
		// Linode list endpoints return a paginated envelope.
		n := 1
		switch v := data.(type) {
		case []map[string]any:
			n = len(v)
		}
		return map[string]any{
			"data":    data,
			"page":    1,
			"pages":   1,
			"results": n,
		}
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		path := r.URL.Path
		// Strip /v4 prefix used by the client host URL.
		path = strings.TrimPrefix(path, "/v4")
		if path == "" {
			path = "/"
		}

		switch {
		case path == "/regions":
			writeJSON(w, 200, paginated([]map[string]any{
				{
					"id":      "us-east",
					"label":   "Newark, NJ",
					"country": "us",
					"status":  "ok",
					"capabilities": []string{
						"Linodes", "NodeBalancers", "Block Storage",
					},
					"resolvers": map[string]string{
						"ipv4": "1.1.1.1",
						"ipv6": "2600::",
					},
					"site_type": "core",
					"monitors": map[string]any{
						"alerts":  []string{},
						"metrics": []string{},
					},
				},
			}))

		case path == "/account":
			writeJSON(w, 200, map[string]any{
				"first_name": "Test",
				"last_name":  "User",
				"email":      "test@example.com",
				"company":    "OTel",
				"address_1":  "123 Main St",
				"city":       "Newark",
				"state":      "NJ",
				"zip":        "07102",
				"country":    "US",
				"balance":    0,
				"euuid":      "E123",
			})

		case path == "/volumes":
			writeJSON(w, 200, paginated([]map[string]any{
				{
					"id":     123,
					"label":  "test-volume",
					"status": "active",
					"region": "us-east",
					"size":   20,
					"tags":   []string{},
				},
			}))

		case strings.HasPrefix(path, "/volumes/"):
			id := strings.TrimPrefix(path, "/volumes/")
			if id == "999" || id == "" {
				writeJSON(w, 404, map[string]any{
					"errors": []map[string]string{{"reason": "Not found"}},
				})
				return
			}
			vid, _ := strconv.Atoi(id)
			writeJSON(w, 200, map[string]any{
				"id":     vid,
				"label":  "test-volume",
				"status": "active",
				"region": "us-east",
				"size":   20,
				"tags":   []string{},
			})

		case path == "/linode/instances":
			writeJSON(w, 200, paginated([]map[string]any{
				{
					"id":     123,
					"label":  "test-instance",
					"region": "us-east",
					"type":   "g6-nanode-1",
					"status": "running",
				},
			}))

		case strings.HasPrefix(path, "/linode/instances/"):
			id := strings.TrimPrefix(path, "/linode/instances/")
			// Only bare instance IDs; ignore subpaths for this mock.
			if strings.Contains(id, "/") {
				writeJSON(w, 404, map[string]any{
					"errors": []map[string]string{{"reason": "Not found"}},
				})
				return
			}
			if id == "999" || id == "" {
				writeJSON(w, 404, map[string]any{
					"errors": []map[string]string{{"reason": "Not found"}},
				})
				return
			}
			iid, _ := strconv.Atoi(id)
			writeJSON(w, 200, map[string]any{
				"id":     iid,
				"label":  "test-instance",
				"region": "us-east",
				"type":   "g6-nanode-1",
				"status": "running",
			})

		default:
			writeJSON(w, 404, map[string]any{
				"errors": []map[string]string{{"reason": "Not found: " + path}},
			})
		}
	}))
	t.Cleanup(server.Close)
	return server
}
