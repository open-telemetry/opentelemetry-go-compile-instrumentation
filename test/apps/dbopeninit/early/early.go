// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package early opens databases in package-level variables. Its import path
// sorts before go.opentelemetry.io, so Go initializes it before the
// database/sql instrumentation, and the sql.Open hooks run that early.
package early

import (
	"database/sql"
	"os"

	_ "go.opentelemetry.io/otelc/test/shared/testdb"
)

var (
	// DB uses a driver name with no DSN parser, so its Open hook reaches the
	// "could not determine server.address" warning while the logger is nil.
	DB = mustOpen("testdb", "user:pass@tcp(127.0.0.1:3306)/testdb")
	// MySQL uses the built-in MySQL DSN parser and its protocol table.
	MySQL = mustOpen("mysql", "user:pass@tcp:3306/inventory")
)

// An os.Exit here runs before pkg/runtime initializes, so the os.Exit hook
// reaches pkg/runtime while its variables are not set yet.
func init() {
	if os.Getenv("DBOPENINIT_EXIT_IN_INIT") != "" {
		os.Exit(0)
	}
}

func mustOpen(driverName, dsn string) *sql.DB {
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		panic(err)
	}
	return db
}
