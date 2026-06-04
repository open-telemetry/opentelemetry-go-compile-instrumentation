// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

// ProcessRequest is a helper that should only be instrumented in production
// (non-test) builds. The is_test: false predicate ensures the rule is skipped
// when the Go toolchain compiles the .test binary.
func ProcessRequest(req string) error {
	println("processing:", req)
	return nil
}
