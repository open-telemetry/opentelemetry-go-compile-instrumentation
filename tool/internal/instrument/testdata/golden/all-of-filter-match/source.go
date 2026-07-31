// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// This is the driver's registration file: it declares the Driver type and a
// package init that would register it. The all-of filter targets Connect here
// (and not the helper Connect in other files) by requiring both markers.
package main

type Driver struct{}

func init() {
	_ = Driver{}
}

func Connect(dsn string) error {
	println("connecting to " + dsn)
	return nil
}
