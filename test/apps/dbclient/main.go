// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a minimal database/sql client for integration testing.
// It uses a custom in-memory driver to avoid external dependencies.
// This client is designed to be instrumented with the otel compile-time tool.
package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
)

var (
	driverName = flag.String("driver", "testdb", "The database driver name")
	dsn        = flag.String("dsn", "user:pass@tcp(127.0.0.1:3306)/testdb?charset=utf8", "The data source name")
	op         = flag.String("op", "all", "The operation to perform: ping, exec, query, tx, prepare, all")
)

func init() {
	sql.Register("testdb", &testDriver{})
}

func main() {
	flag.Parse()

	db, err := sql.Open(*driverName, *dsn)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	switch *op {
	case "ping":
		doPing(ctx, db)
	case "exec":
		doExec(ctx, db)
	case "query":
		doQuery(ctx, db)
	case "tx":
		doTx(ctx, db)
	case "prepare":
		doPrepare(ctx, db)
	case "all":
		doPing(ctx, db)
		doExec(ctx, db)
		doQuery(ctx, db)
		doPrepare(ctx, db)
		doTx(ctx, db)
	default:
		log.Fatalf("unknown operation: %s", *op)
	}

	slog.Info("database operations completed successfully")
}

func doPing(ctx context.Context, db *sql.DB) {
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("failed to ping: %v", err)
	}
	slog.Info("ping succeeded")
}

func doExec(ctx context.Context, db *sql.DB) {
	result, err := db.ExecContext(ctx, "INSERT INTO users (name, email) VALUES (?, ?)", "alice", "alice@example.com")
	if err != nil {
		log.Fatalf("failed to exec: %v", err)
	}
	rows, _ := result.RowsAffected()
	slog.Info("exec succeeded", "rows_affected", rows)
}

func doQuery(ctx context.Context, db *sql.DB) {
	rows, err := db.QueryContext(ctx, "SELECT id, name FROM users WHERE name = ?", "alice")
	if err != nil {
		log.Fatalf("failed to query: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			log.Fatalf("failed to scan: %v", err)
		}
		slog.Info("query result", "id", id, "name", name)
	}
	slog.Info("query succeeded")
}

func doPrepare(ctx context.Context, db *sql.DB) {
	stmt, err := db.PrepareContext(ctx, "SELECT id FROM users WHERE name = ?")
	if err != nil {
		log.Fatalf("failed to prepare: %v", err)
	}
	defer stmt.Close()

	rows, err := stmt.QueryContext(ctx, "alice")
	if err != nil {
		log.Fatalf("failed to query with prepared stmt: %v", err)
	}
	defer rows.Close()
	slog.Info("prepare and stmt query succeeded")
}

func doTx(ctx context.Context, db *sql.DB) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		log.Fatalf("failed to begin tx: %v", err)
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO orders (user_id, amount) VALUES (?, ?)", 1, 99.99)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Fatalf("failed to rollback: %v", rbErr)
		}
		log.Fatalf("failed to exec in tx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		log.Fatalf("failed to commit: %v", err)
	}
	slog.Info("transaction committed")
}

// =============================================================================
// Minimal in-memory SQL driver for testing database/sql instrumentation.
// This driver does not actually store data; it returns canned responses.
// =============================================================================

type testDriver struct{}

func (d *testDriver) Open(name string) (driver.Conn, error) {
	return &testConn{}, nil
}

type testConn struct{}

func (c *testConn) Prepare(query string) (driver.Stmt, error) {
	return &testStmt{query: query}, nil
}

func (c *testConn) Close() error {
	return nil
}

func (c *testConn) Begin() (driver.Tx, error) {
	return &testTx{}, nil
}

func (c *testConn) Ping(ctx context.Context) error {
	return nil
}

// Implement driver.QueryerContext for direct query support
func (c *testConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return &testRows{
		columns: []string{"id", "name"},
		data: [][]driver.Value{
			{int64(1), "alice"},
		},
	}, nil
}

// Implement driver.ExecerContext for direct exec support
func (c *testConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	return &testResult{lastInsertID: 1, rowsAffected: 1}, nil
}

type testStmt struct {
	query string
}

func (s *testStmt) Close() error {
	return nil
}

func (s *testStmt) NumInput() int {
	return -1 // variable number of args
}

func (s *testStmt) Exec(args []driver.Value) (driver.Result, error) {
	return &testResult{lastInsertID: 1, rowsAffected: 1}, nil
}

func (s *testStmt) Query(args []driver.Value) (driver.Rows, error) {
	return &testRows{
		columns: []string{"id", "name"},
		data: [][]driver.Value{
			{int64(1), "alice"},
		},
	}, nil
}

type testResult struct {
	lastInsertID int64
	rowsAffected int64
}

func (r *testResult) LastInsertId() (int64, error) {
	return r.lastInsertID, nil
}

func (r *testResult) RowsAffected() (int64, error) {
	return r.rowsAffected, nil
}

type testTx struct{}

func (t *testTx) Commit() error {
	return nil
}

func (t *testTx) Rollback() error {
	return nil
}

type testRows struct {
	columns []string
	data    [][]driver.Value
	pos     int
}

func (r *testRows) Columns() []string {
	return r.columns
}

func (r *testRows) Close() error {
	return nil
}

func (r *testRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.data) {
		return io.EOF
	}
	row := r.data[r.pos]
	for i, v := range row {
		dest[i] = v
	}
	r.pos++
	return nil
}

func (r *testRows) HasNextResultSet() bool {
	return false
}

func (r *testRows) NextResultSet() error {
	return fmt.Errorf("no more result sets")
}
