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

func TestDBClient(t *testing.T) {
	t.Parallel()

	t.Run("Ping", func(t *testing.T) {
		f := testutil.NewTestFixture(t)

		f.Run("dbclient", "-op=ping")

		span := f.RequireSingleSpan()
		require.Equal(t, "PING", span.Name())
		testutil.RequireDBClientSemconv(t, span,
			"PING",
			"ping",
			"unknown", 0,
			"testdb",
		)
	})

	t.Run("Exec", func(t *testing.T) {
		f := testutil.NewTestFixture(t)

		f.Run("dbclient", "-op=exec")

		span := f.RequireSingleSpan()
		require.Equal(t, "INSERT", span.Name())
		testutil.RequireDBClientSemconv(t, span,
			"INSERT",
			"INSERT INTO users (name, email) VALUES (?, ?)",
			"unknown", 0,
			"testdb",
		)
	})

	t.Run("Query", func(t *testing.T) {
		f := testutil.NewTestFixture(t)

		f.Run("dbclient", "-op=query")

		span := f.RequireSingleSpan()
		require.Equal(t, "SELECT", span.Name())
		testutil.RequireDBClientSemconv(t, span,
			"SELECT",
			"SELECT id, name FROM users WHERE name = ?",
			"unknown", 0,
			"testdb",
		)
	})

	t.Run("PrepareAndQuery", func(t *testing.T) {
		f := testutil.NewTestFixture(t)

		f.Run("dbclient", "-op=prepare")

		// PrepareContext doesn't create a span directly, but stmt.QueryContext does
		spans := testutil.AllSpans(f.Traces())
		require.GreaterOrEqual(t, len(spans), 1, "Expected at least 1 span from prepared statement query")

		// Find the query span from stmt.QueryContext
		stmtSpan := testutil.RequireSpan(t, f.Traces(), testutil.IsClient)
		require.Equal(t, "SELECT", stmtSpan.Name())
	})

	t.Run("Transaction", func(t *testing.T) {
		f := testutil.NewTestFixture(t)

		f.Run("dbclient", "-op=tx")

		spans := testutil.AllSpans(f.Traces())
		// BeginTx -> ExecContext -> Commit = 3 spans
		require.Equal(t, 3, len(spans), "Expected 3 spans for transaction: begin, exec, commit")

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
	})

	t.Run("All", func(t *testing.T) {
		f := testutil.NewTestFixture(t)

		f.Run("dbclient",
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
	})
}

func TestDBClientDSNParsing(t *testing.T) {
	tests := []struct {
		name       string
		driverName string
		dsn        string
		wantAddr   string
		wantPort   int64
		wantDb     string
	}{
		{
			name:       "MySQL standard tcp",
			driverName: "mysql",
			dsn:        "user:pass@tcp(127.0.0.1:3306)/inventory",
			wantAddr:   "127.0.0.1",
			wantPort:   3306,
			wantDb:     "inventory",
		},
		{
			name:       "Postgres URL format with port",
			driverName: "postgres",
			dsn:        "postgres://user:pass@localhost:5432/reporting?sslmode=disable",
			wantAddr:   "localhost",
			wantPort:   5432,
			wantDb:     "reporting",
		},
		{
			name:       "Postgres URL format default port",
			driverName: "postgres",
			dsn:        "postgres://localhost/reporting",
			wantAddr:   "localhost",
			wantPort:   5432,
			wantDb:     "reporting",
		},
		{
			name:       "SQL Server URL format",
			driverName: "sqlserver",
			dsn:        "sqlserver://sa:password@localhost:1433?database=master",
			wantAddr:   "localhost",
			wantPort:   1433,
			wantDb:     "sa:password@localhost:1433", // Current ParseDbName logic for this format
		},
		{
			name:       "SQLite3 local file",
			driverName: "sqlite3",
			dsn:        "file:test.db?cache=shared",
			wantAddr:   "sqlite3",
			wantPort:   0,
			wantDb:     "", // Current ParseDbName logic returns empty if no '/' is found
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := testutil.NewTestFixture(t)
			f.BuildAndRun("dbclient",
				"-driver="+tt.driverName,
				"-dsn="+tt.dsn,
				"-op=ping",
			)

			span := f.RequireSingleSpan()
			testutil.RequireDBClientSemconv(t, span,
				"PING",
				"ping",
				tt.wantAddr, tt.wantPort,
				tt.wantDb,
			)
		})
	}
}
