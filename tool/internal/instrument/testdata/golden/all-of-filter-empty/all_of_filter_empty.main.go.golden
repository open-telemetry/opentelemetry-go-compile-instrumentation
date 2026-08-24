// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// An empty all-of matches every file (vacuous truth): every condition in an
// empty set is trivially satisfied, so the where.file.all-of: [] predicate
// gates nothing out and Open below IS instrumented.
package main

type Driver struct{}

func Open(dsn string) error {
	println("opening " + dsn)
	return nil
}
