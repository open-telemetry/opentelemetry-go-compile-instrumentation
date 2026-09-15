// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package testdb provides a minimal in-memory SQL driver for testing database/sql instrumentation.
// This driver does not actually store data; it returns canned responses.
package testdb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
)

// mysqlDriver is the exact instance registered under "mysql" (as opposed to a
// fresh &testDriver{}), so a driver.Connector wrapping it can be resolved
// back to "mysql" by identity, the way a real connector-based driver like
// go-sql-driver/mysql or pgx/v5/stdlib would be.
var mysqlDriver = &testDriver{}

func init() {
	sql.Register("testdb", &testDriver{})
	sql.Register("mysql", mysqlDriver)
	sql.Register("postgres", &testDriver{})
	sql.Register("postgresql", &testDriver{})
	sql.Register("sqlserver", &testDriver{})
	sql.Register("mssql", &testDriver{})
	sql.Register("sqlite3", &testDriver{})
	sql.Register("testdb-fail", &failTxDriver{})
}

type testDriver struct{}

func (d *testDriver) Open(name string) (driver.Conn, error) {
	return &testConn{}, nil
}

// Connector is a driver.Connector wrapping the registered "mysql" test
// driver and exposing DSN(), for exercising the sql.OpenDB / driver.Connector
// instrumentation path the way a connector-based driver (e.g.
// mysql.NewConnector, pgx/v5/stdlib) is used in practice, as opposed to
// sql.Open's driver-name-string path.
type Connector struct {
	dsn string
}

// NewConnector returns a driver.Connector for sql.OpenDB, backed by the
// registered "mysql" test driver and carrying dsn for DSN().
func NewConnector(dsn string) *Connector {
	return &Connector{dsn: dsn}
}

func (c *Connector) Connect(context.Context) (driver.Conn, error) {
	return mysqlDriver.Open(c.dsn)
}

func (c *Connector) Driver() driver.Driver {
	return mysqlDriver
}

func (c *Connector) DSN() string {
	return c.dsn
}

// failTxDriver is a driver whose connections always fail Begin().
type failTxDriver struct{}

func (d *failTxDriver) Open(name string) (driver.Conn, error) {
	return &failTxConn{}, nil
}

type failTxConn struct{}

func (c *failTxConn) Prepare(query string) (driver.Stmt, error) {
	return nil, fmt.Errorf("failTxConn does not support Prepare")
}

func (c *failTxConn) Close() error {
	return nil
}

func (c *failTxConn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("simulated transaction start failure")
}

func (c *failTxConn) Ping(ctx context.Context) error {
	return nil
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
