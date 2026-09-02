// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// This file is the real Connect implementation. The not filter instruments
// Connect everywhere EXCEPT files that declare a MockConn test double, so that
// hooks never wrap the in-memory mock. This file has no MockConn, so the
// negation holds and Connect IS instrumented here.
package main

func Connect(dsn string) error {
	println("connecting " + dsn)
	return nil
}
