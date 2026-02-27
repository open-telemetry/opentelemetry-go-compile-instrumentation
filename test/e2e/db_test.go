//go:build e2e

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"testing"

	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/testutil"
)

func TestDB(t *testing.T) {
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
	if len(spans) < 7 {
		t.Fatalf("Expected at least 7 spans, got %d", len(spans))
	}

	// For "testdb" driver, parseDSN returns an error (unknown driver),
	// so beforeOpenInstrumentation falls back to "unknown" as the endpoint.
	const serverAddr = "unknown"

	// Verify ping span
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

	// Verify exec span (DB.ExecContext INSERT)
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

	// Verify query span (DB.QueryContext SELECT)
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

	// Verify transaction begin span
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

	// Verify transaction commit span
	commitSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsClient,
		testutil.HasAttribute(string(semconv.DBOperationNameKey), "COMMIT"),
	)
	testutil.RequireDBClientSemconv(t, commitSpan,
		"COMMIT",
		"COMMIT",
		serverAddr, 0,
		"testdb",
	)
}
