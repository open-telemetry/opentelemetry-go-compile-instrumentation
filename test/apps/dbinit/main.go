// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main uses the database after another package used it during
// initialization.
package main

import (
	"context"
	"database/sql"
	"log"

	_ "example.com/dbinit/early"
)

func main() {
	ctx := context.Background()
	db, err := sql.Open("testdb", "user:pass@tcp(127.0.0.1:3306)/testdb")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		log.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO users (name) VALUES ('x')"); err != nil {
		log.Fatal(err)
	}
}
