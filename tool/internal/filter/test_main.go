// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package filter

import "strings"

// Compile-time check that TestMainFilter implements Filter.
var _ Filter = (*TestMainFilter)(nil)

// TestMainFilter matches or excludes test packages based on whether the
// package's import path carries the ".test" suffix that Go appends when
// compiling a test binary.
//
// ShouldMatch == true  → match only test packages (import path ends in ".test")
// ShouldMatch == false → match only non-test packages
//
// This corresponds directly to the YAML field:
//
//	where:
//	  test_main: true   # instrument test packages only
//	  test_main: false  # instrument non-test packages only
type TestMainFilter struct {
	ShouldMatch bool
}

// Match reports whether the test-ness of the source file's package matches
// f.ShouldMatch.
func (f *TestMainFilter) Match(ctx *MatchContext) bool {
	isTest := strings.HasSuffix(ctx.ImportPath, ".test")
	return f.ShouldMatch == isTest
}
