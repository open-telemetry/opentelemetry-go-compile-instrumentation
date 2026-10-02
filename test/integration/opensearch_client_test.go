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

func TestOpenSearchClient(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "opensearchclientv5", "go", "build", "-a")

	f := testutil.NewTestFixture(t)
	os := startOpenSearchMock(t)
	frontPort := testutil.FreePort(t)

	f.Start("opensearchclientv5",
		fmt.Sprintf("-front-port=%d", frontPort),
		"-os-url="+os.URL,
	)
	testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", frontPort))

	searchBody := hitEndpoint(t, frontPort, "/search")
	require.Contains(t, searchBody, "search status=")

	f.WaitForSpans(2)
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
	testutil.RequireOpenSearchClientSemconv(
		t,
		searchClient,
		"search",
		"orders",
		"POST",
		"/orders/_search",
		200,
	)

	indexBody := hitEndpoint(t, frontPort, "/index")
	require.Contains(t, indexBody, "id=1")

	f.WaitForSpans(4)
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
	testutil.RequireOpenSearchClientSemconv(
		t,
		indexClient,
		"index",
		"orders",
		"POST",
		"/orders/_doc/1",
		200,
	)

	deleteBody := hitEndpoint(t, frontPort, "/delete")
	require.Contains(t, deleteBody, "id=1")

	f.WaitForSpans(6)
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
	testutil.RequireOpenSearchClientSemconv(
		t,
		deleteClient,
		"delete",
		"orders",
		"DELETE",
		"/orders/_doc/1",
		200,
	)
}

func TestOpenSearchClient_Disabled(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "opensearchclientv5", "go", "build", "-a")

	f := testutil.NewTestFixture(t)
	f.SetEnv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "opensearch")
	os := startOpenSearchMock(t)
	frontPort := testutil.FreePort(t)

	f.Start("opensearchclientv5",
		fmt.Sprintf("-front-port=%d", frontPort),
		"-os-url="+os.URL,
	)
	testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", frontPort))

	searchBody := hitEndpoint(t, frontPort, "/search")
	require.Contains(t, searchBody, "search status=")
	indexBody := hitEndpoint(t, frontPort, "/index")
	require.Contains(t, indexBody, "id=1")

	f.WaitForSpans(4)
	var (
		clientSpans    int
		endpointSpans  int
		unexpectedSpan []string
	)
	for _, span := range testutil.AllSpans(f.Traces()) {
		require.False(t, testutil.HasAttribute("db.system.name", "opensearch")(span),
			"disabled hook must not emit an opensearch client span")
		if testutil.IsServer(span) && (span.Name() == "GET /search" || span.Name() == "GET /index") {
			endpointSpans++
			continue
		}
		if testutil.IsClient(span) {
			attrs := testutil.Attrs(span)
			if url, ok := attrs["url.full"].(string); ok && strings.Contains(url, os.URL) {
				clientSpans++
				continue
			}
		}
		unexpectedSpan = append(unexpectedSpan, fmt.Sprintf("%s %v", span.Name(), testutil.Attrs(span)))
	}
	require.Equal(t, 2, endpointSpans)
	require.GreaterOrEqual(t, clientSpans, 2, "inner net/http client spans must come back when opensearch is disabled")
	require.Empty(t, unexpectedSpan, "unexpected spans: %v", unexpectedSpan)
}

func startOpenSearchMock(t *testing.T) *httptest.Server {
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
			result := "indexed"
			if r.Method == http.MethodDelete {
				result = "deleted"
			}
			_, _ = fmt.Fprintf(w, `{
				"_index":"orders",
				"_id":"1",
				"_version":1,
				"result":%q,
				"_shards":{"total":1,"successful":1,"failed":0},
				"_seq_no":0,
				"_primary_term":1
			}`, result)
		default:
			_, _ = io.WriteString(w, `{"name":"mock","cluster_name":"mock"}`)
		}
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}
