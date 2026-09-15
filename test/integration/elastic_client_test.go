// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"go.opentelemetry.io/otelc/test/testutil"
)

func TestElasticClient(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "elasticclient", "go", "build", "-a")

	f := testutil.NewTestFixture(t)
	es := startElasticsearchMock(t)
	frontPort := testutil.FreePort(t)

	f.Start("elasticclient",
		fmt.Sprintf("-front-port=%d", frontPort),
		"-es-url="+es.URL,
	)
	testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", frontPort))

	searchBody := hitEndpoint(t, frontPort, "/search")
	require.Contains(t, searchBody, "hits=")

	f.WaitForSpans(2)
	require.Len(t, testutil.AllSpans(f.Traces()), 2)
	searchServer := testutil.RequireSpan(
		t,
		f.Traces(),
		testutil.IsServer,
		func(s ptrace.Span) bool { return s.Name() == "GET /search" },
	)
	searchClient := testutil.RequireSpan(
		t,
		f.Traces(),
		testutil.IsClient,
		testutil.HasAttribute("db.operation.name", "search"),
	)
	require.Equal(t, searchServer.TraceID(), searchClient.TraceID())
	require.Equal(t, searchServer.SpanID(), searchClient.ParentSpanID())
	testutil.RequireElasticsearchClientSemconv(
		t,
		searchClient,
		"search",
		"orders",
		"POST",
		"/orders/_search",
		200,
	)

	indexBody := hitEndpoint(t, frontPort, "/index")
	require.Contains(t, indexBody, "result=created")

	f.WaitForSpans(4)
	require.Len(t, testutil.AllSpans(f.Traces()), 4)
	indexServer := testutil.RequireSpan(
		t,
		f.Traces(),
		testutil.IsServer,
		func(s ptrace.Span) bool { return s.Name() == "GET /index" },
	)
	indexClient := testutil.RequireSpan(
		t,
		f.Traces(),
		testutil.IsClient,
		testutil.HasAttribute("db.operation.name", "index"),
	)
	require.Equal(t, indexServer.TraceID(), indexClient.TraceID())
	require.Equal(t, indexServer.SpanID(), indexClient.ParentSpanID())
	testutil.RequireElasticsearchClientSemconv(
		t,
		indexClient,
		"index",
		"orders",
		"PUT",
		"/orders/_doc/1",
		200,
	)

	deleteBody := hitEndpoint(t, frontPort, "/delete")
	require.Contains(t, deleteBody, "id=1")

	f.WaitForSpans(6)
	require.Len(t, testutil.AllSpans(f.Traces()), 6)
	deleteServer := testutil.RequireSpan(
		t,
		f.Traces(),
		testutil.IsServer,
		func(s ptrace.Span) bool { return s.Name() == "GET /delete" },
	)
	deleteClient := testutil.RequireSpan(
		t,
		f.Traces(),
		testutil.IsClient,
		testutil.HasAttribute("db.operation.name", "delete"),
	)
	require.Equal(t, deleteServer.TraceID(), deleteClient.TraceID())
	require.Equal(t, deleteServer.SpanID(), deleteClient.ParentSpanID())
	testutil.RequireElasticsearchClientSemconv(
		t,
		deleteClient,
		"delete",
		"orders",
		"DELETE",
		"/orders/_doc/1",
		200,
	)
}

func TestElasticClient_Disabled(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "elasticclient", "go", "build", "-a")

	f := testutil.NewTestFixture(t)
	f.SetEnv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "elastic")
	es := startElasticsearchMock(t)
	frontPort := testutil.FreePort(t)

	f.Start("elasticclient",
		fmt.Sprintf("-front-port=%d", frontPort),
		"-es-url="+es.URL,
	)
	testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", frontPort))

	searchBody := hitEndpoint(t, frontPort, "/search")
	require.Contains(t, searchBody, "hits=")
	indexBody := hitEndpoint(t, frontPort, "/index")
	require.Contains(t, indexBody, "result=created")

	f.WaitForSpans(4)
	require.Len(t, testutil.AllSpans(f.Traces()), 4)
	var clientSpans int
	for _, span := range testutil.AllSpans(f.Traces()) {
		require.False(t, testutil.HasAttribute("db.system.name", "elasticsearch")(span),
			"disabled hook must not emit an elasticsearch client span")
		if testutil.IsClient(span) {
			clientSpans++
		}
	}
	require.Positive(t, clientSpans, "inner net/http client spans must come back when elastic is disabled")
}

func hitEndpoint(t *testing.T, port int, path string) string {
	t.Helper()
	url := fmt.Sprintf("http://127.0.0.1:%d%s", port, path)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "body=%s", body)
	return string(body)
}

func startElasticsearchMock(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/_search"):
			_, _ = io.WriteString(w, `{
				"took":1,
				"timed_out":false,
				"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},
				"hits":{"total":{"value":1,"relation":"eq"},"max_score":1.0,"hits":[]}
			}`)
		case strings.Contains(r.URL.Path, "/_doc"):
			_, _ = io.WriteString(w, `{
				"_index":"orders",
				"_type":"_doc",
				"_id":"1",
				"_version":1,
				"result":"created",
				"_shards":{"total":1,"successful":1,"failed":0},
				"_seq_no":0,
				"_primary_term":1
			}`)
		default:
			_, _ = io.WriteString(w, `{"name":"mock","cluster_name":"mock","tagline":"You Know, for Search"}`)
		}
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}
