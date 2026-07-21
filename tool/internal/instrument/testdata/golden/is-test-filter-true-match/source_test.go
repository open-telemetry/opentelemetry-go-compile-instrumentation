// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

// ProcessRequest lives in a _test.go file, so the Go toolchain compiles it only
// as part of `go test`. The is_test: true rule matches that test build and the
// golden captures the injected trampolines — the positive counterpart to
// is-test-filter-no-match, where the same rule is gated out of a normal build.
func ProcessRequest(req string) error {
	println("processing:", req)
	return nil
}
