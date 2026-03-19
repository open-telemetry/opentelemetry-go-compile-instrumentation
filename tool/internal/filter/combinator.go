// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package filter

// Compile-time checks that combinators implement Filter.
var (
	_ Filter = (AllOf)(nil)
	_ Filter = (OneOf)(nil)
)

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

// OneOf is a Filter combinator that matches when at least one child filter
// matches. An empty OneOf returns false (no condition can be satisfied).
type OneOf []Filter

// Match reports whether any child filter matches ctx.
// It short-circuits on the first matching child.
func (o OneOf) Match(ctx *MatchContext) bool {
	for _, f := range o {
		if f.Match(ctx) {
			return true
		}
	}
	return false
}
