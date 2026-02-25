// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/testutil"
)

func TestDBClientPing(t *testing.T) {
	f := testutil.NewTestFixture(t)

	f.BuildAndRun("dbclient", "-op=ping")

	span := f.RequireSingleSpan()
	require.Equal(t, "ping", span.Name())
	testutil.RequireDBClientSemconv(t, span,
		"ping",
		"ping",
		"user:pass@tcp(127.0.0.1:3306)/testdb?charset=utf8", 0,
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
		"user:pass@tcp(127.0.0.1:3306)/testdb?charset=utf8", 0,
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
		"user:pass@tcp(127.0.0.1:3306)/testdb?charset=utf8", 0,
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
