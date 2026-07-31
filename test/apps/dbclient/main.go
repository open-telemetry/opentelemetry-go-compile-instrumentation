// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a minimal database/sql client for integration testing.
// It uses a custom in-memory driver to avoid external dependencies.
// This client is designed to be instrumented with the otelc compile-time tool.
package main

import (
	"context"
	"database/sql"
	"flag"
	"log"
	"log/slog"

	_ "go.opentelemetry.io/otelc/test/shared/testdb"
)

var (
	driverName = flag.String("driver", "testdb", "The database driver name")
	dsn        = flag.String("dsn", "user:pass@tcp(127.0.0.1:3306)/testdb?charset=utf8", "The data source name")
	op         = flag.String("op", "all", "The operation to perform: ping, exec, query, tx, prepare, all")
)

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
