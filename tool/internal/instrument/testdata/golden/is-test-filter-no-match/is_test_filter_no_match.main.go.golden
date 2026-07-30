// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

// ProcessRequest is targeted by a test-only rule (is_test: true). This fixture
// is a normal build — its source is source.go, not a _test.go file — so the
// rule is gated out and the output stays byte-identical to this source. The
// matching counterpart is is-test-filter-true-match.
func ProcessRequest(req string) error {
	println("processing:", req)
	return nil
}
