// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// This file declares a MockConn test double alongside its own Connect. Because
// the not filter excludes files that declare MockConn, the negation fails here
// and the same rule leaves this Connect untouched (no trampoline injection).
package main

type MockConn struct{}

func Connect(dsn string) error {
	println("connecting " + dsn)
	return nil
}
