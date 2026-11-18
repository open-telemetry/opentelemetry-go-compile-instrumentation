// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"sync"

	"go.opentelemetry.io/otelc/test/shared/testdb"
)

type rowsDriver struct {
	mode    string
	cancel  context.CancelFunc
	reading chan struct{}
}

func (fixture *rowsDriver) Open(dsn string) (driver.Conn, error) {
	base, err := testdb.NewConnector(dsn).Connect(context.Background())
	if err != nil {
		return nil, err
	}
	return &rowsConn{Conn: base, fixture: fixture}, nil
}

func (fixture *rowsDriver) query(ctx context.Context) (driver.Rows, error) {
	if fixture.mode == "query-error" {
		return nil, errors.New("query failed")
	}
	if fixture.mode == "cancel-before-return" {
		fixture.cancel()
	}
	return &fixtureRows{fixture: fixture, ctx: ctx}, nil
}

type rowsConn struct {
	driver.Conn
	fixture *rowsDriver
}

func (conn *rowsConn) QueryContext(
	ctx context.Context, _ string, _ []driver.NamedValue,
) (driver.Rows, error) {
	return conn.fixture.query(ctx)
}

func (conn *rowsConn) Prepare(query string) (driver.Stmt, error) {
	base, err := conn.Conn.Prepare(query)
	if err != nil {
		return nil, err
	}
	return &rowsStmt{Stmt: base, fixture: conn.fixture}, nil
}

type rowsStmt struct {
	driver.Stmt
	fixture *rowsDriver
}

func (stmt *rowsStmt) QueryContext(ctx context.Context, _ []driver.NamedValue) (driver.Rows, error) {
	return stmt.fixture.query(ctx)
}

type fixtureRows struct {
	fixture *rowsDriver
	ctx     context.Context
	pos     int
	set     int
	closed  sync.Once
}

func (*fixtureRows) Columns() []string { return []string{"id"} }

func (rows *fixtureRows) Close() error {
	if rows.fixture.mode == "close-error" {
		return errors.New("close failed")
	}
	return nil
}

func (rows *fixtureRows) Next(dest []driver.Value) error {
	switch rows.fixture.mode {
	case "empty":
		return io.EOF
	case "next-error":
		if rows.pos == 1 {
			return errors.New("next failed")
		}
	case "cancel":
		if rows.pos == 1 {
			rows.closed.Do(func() { close(rows.fixture.reading) })
			<-rows.ctx.Done()
			return rows.ctx.Err()
		}
	}
	limit := 3
	if rows.fixture.mode == "multiple" || rows.fixture.mode == "next-result-error" {
		limit = []int{2, 0, 1}[rows.set]
	}
	if rows.pos >= limit {
		return io.EOF
	}
	rows.pos++
	dest[0] = int64(rows.pos)
	return nil
}

func (rows *fixtureRows) HasNextResultSet() bool {
	return (rows.fixture.mode == "multiple" || rows.fixture.mode == "next-result-error") && rows.set < 2
}

func (rows *fixtureRows) NextResultSet() error {
	if rows.fixture.mode == "next-result-error" {
		return errors.New("next result failed")
	}
	if !rows.HasNextResultSet() {
		return io.EOF
	}
	rows.set++
	rows.pos = 0
	return nil
}
