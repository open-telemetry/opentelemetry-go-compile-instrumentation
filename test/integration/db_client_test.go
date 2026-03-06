// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"testing"

	"github.com/stretchr/testify/require"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/testutil"
)

func TestDBClientPing(t *testing.T) {
	f := testutil.NewTestFixture(t)

	f.BuildAndRun("dbclient", "-op=ping")

	span := f.RequireSingleSpan()
	require.Equal(t, "PING", span.Name())
	testutil.RequireDBClientSemconv(t, span,
		"PING",
		"ping",
		"unknown", 0,
		"testdb",
	)
}

func TestDBClientExec(t *testing.T) {
	f := testutil.NewTestFixture(t)

	f.BuildAndRun("dbclient", "-op=exec")

	span := f.RequireSingleSpan()
	require.Equal(t, "INSERT", span.Name())
	testutil.RequireDBClientSemconv(t, span,
		"INSERT",
		"INSERT INTO users (name, email) VALUES (?, ?)",
		"unknown", 0,
		"testdb",
	)
}

func TestDBClientQuery(t *testing.T) {
	f := testutil.NewTestFixture(t)

	f.BuildAndRun("dbclient", "-op=query")

	span := f.RequireSingleSpan()
	require.Equal(t, "SELECT", span.Name())
	testutil.RequireDBClientSemconv(t, span,
		"SELECT",
		"SELECT id, name FROM users WHERE name = ?",
		"unknown", 0,
		"testdb",
	)
}

func TestDBClientPrepareAndQuery(t *testing.T) {
	f := testutil.NewTestFixture(t)

	f.BuildAndRun("dbclient", "-op=prepare")

	// PrepareContext doesn't create a span directly, but stmt.QueryContext does
	spans := testutil.AllSpans(f.Traces())
	require.GreaterOrEqual(t, len(spans), 1, "Expected at least 1 span from prepared statement query")

	// Find the query span from stmt.QueryContext
	stmtSpan := testutil.RequireSpan(t, f.Traces(), testutil.IsClient)
	require.Equal(t, "SELECT", stmtSpan.Name())
}

func TestDBClientTransaction(t *testing.T) {
	f := testutil.NewTestFixture(t)

	f.BuildAndRun("dbclient", "-op=tx")

	spans := testutil.AllSpans(f.Traces())
	// BeginTx -> ExecContext -> Commit = 3 spans
	require.Equal(t, 3, len(spans), "Expected 3 spans for transaction: begin, exec, commit")

	// Verify we have the expected span types
	beginSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsClient,
		testutil.HasAttribute("db.operation.name", "START"),
	)
	require.Equal(t, "START", beginSpan.Name())

	execSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsClient,
		testutil.HasAttribute("db.operation.name", "INSERT"),
	)
	require.Equal(t, "INSERT", execSpan.Name())

	commitSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsClient,
		testutil.HasAttribute("db.operation.name", "COMMIT"),
	)
	require.Equal(t, "COMMIT", commitSpan.Name())
}

func TestDBClientAll(t *testing.T) {
	f := testutil.NewTestFixture(t)

	f.BuildAndRun("dbclient",
		"-driver=testdb",
		"-dsn=user:pass@tcp(127.0.0.1:3306)/testdb?charset=utf8",
		"-op=all",
	)

	// "all" operation produces 7 spans:
	//   PING (PingContext)
	//   INSERT (ExecContext)
	//   SELECT (QueryContext)
	//   SELECT (Stmt.QueryContext via PrepareContext)
	//   START (BeginTx)
	//   INSERT (Tx.ExecContext)
	//   COMMIT (Tx.Commit)
	spans := testutil.AllSpans(f.Traces())
	require.GreaterOrEqual(t, len(spans), 7, "Expected at least 7 spans")

	// For "testdb" driver, parseDSN returns an error (unknown driver),
	// so beforeOpenInstrumentation falls back to "unknown" as the endpoint.
	const serverAddr = "unknown"

	pingSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsClient,
		testutil.HasAttribute(string(semconv.DBOperationNameKey), "PING"),
	)
	testutil.RequireDBClientSemconv(t, pingSpan,
		"PING",
		"ping",
		serverAddr, 0,
		"testdb",
	)

	execSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsClient,
		testutil.HasAttribute(string(semconv.DBQueryTextKey), "INSERT INTO users (name, email) VALUES (?, ?)"),
	)
	testutil.RequireDBClientSemconv(t, execSpan,
		"INSERT",
		"INSERT INTO users (name, email) VALUES (?, ?)",
		serverAddr, 0,
		"testdb",
	)

	querySpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsClient,
		testutil.HasAttribute(string(semconv.DBQueryTextKey), "SELECT id, name FROM users WHERE name = ?"),
	)
	testutil.RequireDBClientSemconv(t, querySpan,
		"SELECT",
		"SELECT id, name FROM users WHERE name = ?",
		serverAddr, 0,
		"testdb",
	)

	beginSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsClient,
		testutil.HasAttribute(string(semconv.DBOperationNameKey), "START"),
	)
	testutil.RequireDBClientSemconv(t, beginSpan,
		"START",
		"START TRANSACTION",
		serverAddr, 0,
		"testdb",
	)

	commitSpan2 := testutil.RequireSpan(t, f.Traces(),
		testutil.IsClient,
		testutil.HasAttribute(string(semconv.DBOperationNameKey), "COMMIT"),
	)
	testutil.RequireDBClientSemconv(t, commitSpan2,
		"COMMIT",
		"COMMIT",
		serverAddr, 0,
		"testdb",
	)
}
