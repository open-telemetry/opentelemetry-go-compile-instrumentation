// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main runs one database/sql transaction under a chosen parent span.
// It imports the OTel SDK so the SDK trace instrumentation, and with it
// goroutine-local span propagation, is enabled.
package main

import (
	"context"
	"database/sql"
	"flag"
	"log"
	"log/slog"

	"go.opentelemetry.io/otel"
	_ "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	_ "go.opentelemetry.io/otelc/test/shared/testdb"
)

var (
	parent = flag.String("parent", "none",
		"parent of the transaction: none, explicit (span in the BeginTx context), "+
			"or implicit (span only in goroutine-local storage)")
	begin = flag.String("begin", "db", "BeginTx receiver: db or conn")
	end   = flag.String("end", "commit", "how the transaction ends: commit or rollback")
)

func main() {
	flag.Parse()

	db, err := sql.Open("testdb", "user:pass@tcp(127.0.0.1:3306)/testdb")
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	var parentSpan trace.Span
	switch *parent {
	case "none":
	case "explicit":
		ctx, parentSpan = otel.Tracer("dbclienttx").Start(ctx, "tx-parent")
	case "implicit", "implicit-ended":
		_, parentSpan = otel.Tracer("dbclienttx").Start(ctx, "tx-parent")
	default:
		log.Fatalf("unknown parent: %s", *parent)
	}

	tx, conn := beginTx(ctx, db)
	if conn != nil {
		defer conn.Close()
	}
	if *parent == "implicit-ended" {
		parentSpan.End()
		parentSpan = nil
	}
	switch *end {
	case "commit":
		err = tx.Commit()
	case "rollback":
		err = tx.Rollback()
	default:
		log.Fatalf("unknown end: %s", *end)
	}
	if err != nil {
		log.Fatalf("failed to %s: %v", *end, err)
	}

	if parentSpan != nil {
		parentSpan.End()
	}
	slog.Info("transaction finished", "parent", *parent, "begin", *begin, "end", *end)
}

func beginTx(ctx context.Context, db *sql.DB) (*sql.Tx, *sql.Conn) {
	switch *begin {
	case "db":
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			log.Fatalf("failed to begin tx: %v", err)
		}
		return tx, nil
	case "conn":
		conn, err := db.Conn(ctx)
		if err != nil {
			log.Fatalf("failed to get conn: %v", err)
		}
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			log.Fatalf("failed to begin tx: %v", err)
		}
		return tx, conn
	default:
		log.Fatalf("unknown begin: %s", *begin)
		return nil, nil
	}
}
