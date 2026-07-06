// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main_test

// ExternalHelper lives in an external test package (package main_test). The
// rule uses a glob target (main*) that covers both the main and main_test
// compiles, then narrows to this package via all-of: [is_test: true,
// has_package: main_test]. This is the realistic discriminating case: the
// external test package compiles at import path main_test, the internal test
// package at main — has_package selects one when the glob matches both.
func ExternalHelper() error {
	println("external helper")
	return nil
}
