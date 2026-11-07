// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"fmt"
	"runtime"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"go.opentelemetry.io/otelc/test/testutil"
)

const clickhouseScope = "go.opentelemetry.io/otelc/instrumentation/github.com/ClickHouse/clickhouse-go/v2"

func TestClickHouseClient(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("clickhouse testcontainer not supported on windows")
	}
	testcontainers.SkipIfProviderIsNotHealthy(t)

	t.Parallel()
	testutil.Build(t, "", "clickhouseclient", "go", "build", "-a")

	host, port := startClickHouseContainer(t)

	f := testutil.NewTestFixture(t)
	output := f.Run("clickhouseclient",
		"-addr="+host+":"+strconv.Itoa(port),
		"-database=testdb", "-user=otelc", "-password=otelc", "-rows=500")
	require.Contains(t, output, "ClickHouse operations completed successfully")

	spans := clickhouseSpans(f.Traces())
	for _, s := range spans {
		require.Equal(t, ptrace.SpanKindClient, s.Kind(), "span %q", s.Name())
		testutil.RequireAttribute(t, s, "db.system.name", "clickhouse")
		testutil.RequireAttribute(t, s, "db.namespace", "testdb")
		testutil.RequireAttribute(t, s, "server.address", host)
		testutil.RequireAttribute(t, s, "server.port", int64(port))
	}

	ping := requireOneSpan(t, spans, func(s ptrace.Span) bool { return s.Name() == "PING testdb" })
	testutil.RequireAttribute(t, ping, "db.operation.name", "PING")

	create := requireOneSpan(
		t,
		spans,
		hasQuery("CREATE TABLE IF NOT EXISTS events (id UInt32, name String) ENGINE = Memory"),
	)
	require.Equal(t, "CREATE testdb", create.Name())

	// The native batch: one PrepareBatch span and one Send span, whatever the
	// number of rows. The database/sql batch, which clickhouse-go also sends
	// with (*batch).Send, gets no span from this instrumentation.
	inserts := filterSpans(spans, hasQuery("INSERT INTO events"))
	require.Len(t, inserts, 2, "PrepareBatch and Send of the native batch only")
	for _, s := range inserts {
		require.Equal(t, "INSERT testdb", s.Name())
		testutil.RequireAttribute(t, s, "db.operation.name", "INSERT")
	}

	requireOneSpan(t, spans, hasQuery("SELECT count() FROM events"))

	// Select calls Query; only the Select span is recorded.
	requireOneSpan(t, spans, hasQuery("SELECT DISTINCT name FROM events"))

	query := requireOneSpan(t, spans, hasQuery("SELECT id FROM events WHERE id < ?"))
	require.Equal(t, "SELECT testdb", query.Name())

	failed := requireOneSpan(t, spans, hasQuery("SELECT * FROM missing_table"))
	require.Equal(t, ptrace.StatusCodeError, failed.Status().Code())
	// 60 is ClickHouse's UNKNOWN_TABLE.
	testutil.RequireAttribute(t, failed, "error.type", "60")
	testutil.RequireAttribute(t, failed, "db.response.status_code", "60")

	require.Len(t, spans, 8, "one span per native call and per batch Send")
}

func startClickHouseContainer(t *testing.T) (string, int) {
	t.Helper()

	ctr, err := testcontainers.Run(t.Context(), "clickhouse/clickhouse-server:25.8-alpine",
		testcontainers.WithExposedPorts("9000/tcp", "8123/tcp"),
		testcontainers.WithEnv(map[string]string{
			"CLICKHOUSE_DB":       "testdb",
			"CLICKHOUSE_USER":     "otelc",
			"CLICKHOUSE_PASSWORD": "otelc",
		}),
		testcontainers.WithWaitStrategy(
			wait.ForHTTP("/ping").WithPort("8123/tcp"),
			wait.ForListeningPort("9000/tcp"),
		),
	)
	testcontainers.CleanupContainer(t, ctr)
	require.NoError(t, err)

	host, err := ctr.Host(t.Context())
	require.NoError(t, err)
	mapped, err := ctr.MappedPort(t.Context(), "9000/tcp")
	require.NoError(t, err)
	return host, int(mapped.Num())
}

// clickhouseSpans returns the spans of the clickhouse-go instrumentation
// scope, leaving out those of the database/sql instrumentation.
func clickhouseSpans(td ptrace.Traces) []ptrace.Span {
	var spans []ptrace.Span
	for i := range td.ResourceSpans().Len() {
		rs := td.ResourceSpans().At(i)
		for j := range rs.ScopeSpans().Len() {
			ss := rs.ScopeSpans().At(j)
			if ss.Scope().Name() != clickhouseScope {
				continue
			}
			for k := range ss.Spans().Len() {
				spans = append(spans, ss.Spans().At(k))
			}
		}
	}
	return spans
}

func hasQuery(q string) func(ptrace.Span) bool {
	return func(s ptrace.Span) bool { return testutil.Attrs(s)["db.query.text"] == q }
}

func filterSpans(spans []ptrace.Span, keep func(ptrace.Span) bool) []ptrace.Span {
	var out []ptrace.Span
	for _, s := range spans {
		if keep(s) {
			out = append(out, s)
		}
	}
	return out
}

func requireOneSpan(t *testing.T, spans []ptrace.Span, keep func(ptrace.Span) bool) ptrace.Span {
	t.Helper()
	matched := filterSpans(spans, keep)
	names := make([]string, 0, len(spans))
	for _, s := range spans {
		names = append(names, fmt.Sprintf("%s %q", s.Name(), testutil.Attrs(s)["db.query.text"]))
	}
	require.Len(t, matched, 1, "spans: %v", names)
	return matched[0]
}
