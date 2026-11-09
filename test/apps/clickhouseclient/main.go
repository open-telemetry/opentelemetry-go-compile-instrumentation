// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a minimal ClickHouse client for integration testing.
// This client is designed to be instrumented with the otelc compile-time tool.
//
// It uses the native API (clickhouse.Open), whose calls the clickhouse-go
// instrumentation traces, and then inserts a batch through database/sql
// (clickhouse.OpenDB), whose batch must not be traced a second time by it.
package main

import (
	"context"
	"flag"
	"log"
	"log/slog"

	"github.com/ClickHouse/clickhouse-go/v2"
)

var (
	addr     = flag.String("addr", "localhost:9000", "The ClickHouse native protocol address")
	database = flag.String("database", "default", "The database to use")
	user     = flag.String("user", "default", "The user name")
	password = flag.String("password", "", "The password")
	rows     = flag.Int("rows", 100, "The number of rows to append to the native batch")
)

func main() {
	flag.Parse()

	ctx := context.Background()
	opts := &clickhouse.Options{
		Addr: []string{*addr},
		Auth: clickhouse.Auth{Database: *database, Username: *user, Password: *password},
	}

	conn, err := clickhouse.Open(opts)
	if err != nil {
		log.Fatalf("failed to open: %v", err)
	}
	defer conn.Close()

	if err := conn.Ping(ctx); err != nil {
		log.Fatalf("failed to ping: %v", err)
	}

	if err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS events (id UInt32, name String) ENGINE = Memory`); err != nil {
		log.Fatalf("failed to create table: %v", err)
	}

	batch, err := conn.PrepareBatch(ctx, `INSERT INTO events`)
	if err != nil {
		log.Fatalf("failed to prepare batch: %v", err)
	}
	for i := range *rows {
		if err := batch.Append(uint32(i), "native"); err != nil {
			log.Fatalf("failed to append: %v", err)
		}
	}
	if err := batch.Send(); err != nil {
		log.Fatalf("failed to send batch: %v", err)
	}
	slog.Info("BATCH", "rows", *rows)

	var count uint64
	if err := conn.QueryRow(ctx, `SELECT count() FROM events`).Scan(&count); err != nil {
		log.Fatalf("failed to count: %v", err)
	}
	slog.Info("COUNT", "count", count)

	var names []struct {
		Name string `ch:"name"`
	}
	if err := conn.Select(ctx, &names, `SELECT DISTINCT name FROM events`); err != nil {
		log.Fatalf("failed to select: %v", err)
	}
	slog.Info("SELECT", "names", len(names))

	result, err := conn.Query(ctx, `SELECT id FROM events WHERE id < ?`, uint32(3))
	if err != nil {
		log.Fatalf("failed to query: %v", err)
	}
	for result.Next() {
		var id uint32
		if err := result.Scan(&id); err != nil {
			log.Fatalf("failed to scan: %v", err)
		}
	}
	result.Close()

	if err := conn.Exec(ctx, `SELECT * FROM missing_table`); err == nil {
		log.Fatalf("query on a missing table succeeded")
	}

	insertThroughDatabaseSQL(opts)

	slog.Info("ClickHouse operations completed successfully")
}

// insertThroughDatabaseSQL inserts one batch through database/sql, which
// clickhouse-go also implements with a batch that has a Send method.
func insertThroughDatabaseSQL(opts *clickhouse.Options) {
	db := clickhouse.OpenDB(opts)
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		log.Fatalf("failed to begin: %v", err)
	}
	stmt, err := tx.Prepare(`INSERT INTO events`)
	if err != nil {
		log.Fatalf("failed to prepare statement: %v", err)
	}
	if _, err := stmt.Exec(uint32(1000), "database/sql"); err != nil {
		log.Fatalf("failed to exec statement: %v", err)
	}
	if err := tx.Commit(); err != nil {
		log.Fatalf("failed to commit: %v", err)
	}
	slog.Info("SQL BATCH", "rows", 1)
}
