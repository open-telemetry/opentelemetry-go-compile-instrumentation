// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// This package supports multiple SQL backends behind one Open entry point. The
// one-of filter instruments Open in any file that declares one of the known
// backend driver types. This file declares PostgresDriver, so Open is matched
// here even though the MySQLDriver type lives in a different backend file.
package main

type PostgresDriver struct{}

func Open(dsn string) error {
	println("opening " + dsn)
	return nil
}
