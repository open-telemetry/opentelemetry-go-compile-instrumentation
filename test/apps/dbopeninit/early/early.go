// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package early opens a database in a package-level variable. Its import path
// sorts before go.opentelemetry.io, so Go initializes it before the
// database/sql instrumentation, and the sql.Open hooks run that early.
package early

import (
	"database/sql"

	_ "go.opentelemetry.io/otelc/test/shared/testdb"
)

// DB is opened during package initialization and used later from main.
var DB = mustOpen()

func mustOpen() *sql.DB {
	db, err := sql.Open("testdb", "user:pass@tcp(127.0.0.1:3306)/testdb")
	if err != nil {
		panic(err)
	}
	return db
}
