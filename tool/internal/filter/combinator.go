// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package filter

// Compile-time check that AllOf implements Filter.
var _ Filter = (AllOf)(nil)

// AllOf is a Filter combinator that matches when all child filters match.
// An empty AllOf returns true (vacuous truth: all conditions in an empty set
// are satisfied).
type AllOf []Filter

// Match reports whether all child filters match ctx.
// It short-circuits on the first non-matching child.
func (a AllOf) Match(ctx *MatchContext) bool {
	for _, f := range a {
		if !f.Match(ctx) {
			return false
		}
	}
	return true
}
