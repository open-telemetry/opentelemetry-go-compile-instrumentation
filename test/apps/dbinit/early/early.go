// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package early uses the database during package initialization. Its import
// path sorts before go.opentelemetry.io, so it initializes before the
// OpenTelemetry packages.
package early

import (
	"context"
	"database/sql"
	"log"

	_ "go.opentelemetry.io/otelc/test/shared/testdb"
)

func init() {
	db, err := sql.Open("testdb", "user:pass@tcp(127.0.0.1:3306)/testdb")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := db.PingContext(context.Background()); err != nil {
		log.Fatal(err)
	}
}
