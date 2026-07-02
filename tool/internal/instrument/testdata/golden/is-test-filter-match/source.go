// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

// ProcessRequest is instrumented only in non-test builds. The is_test: false
// predicate matches this normal build (source.go, no _test.go file); the same
// rule is gated out of a test build. See is-test-filter-true-match for the
// is_test: true counterpart.
func ProcessRequest(req string) error {
	println("processing:", req)
	return nil
}
