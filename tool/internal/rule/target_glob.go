// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
)

// Glob metacharacters recognised in a rule target. A target containing any of
// these is treated as a doublestar glob pattern; otherwise it is an exact
// import path that keeps the fast map-lookup matching path.
const globMeta = "*?[{"

// IsGlobTarget reports whether target uses glob syntax and therefore must be
// matched against every dependency import path rather than looked up by exact
// key. An empty target is not a glob.
func IsGlobTarget(target string) bool {
	return strings.ContainsAny(target, globMeta)
}

// ValidateTarget rejects malformed glob targets at load time so that a bad rule
// fails loudly during parsing rather than silently matching nothing during the
// setup phase. Pattern syntax is bmatcuk/doublestar's; see
// https://github.com/bmatcuk/doublestar#patterns for the full grammar.
//
// An empty target is rejected upstream by the rule loader (parseRuleFromYaml)
// before it reaches ValidateTarget; a non-glob target is a literal import path
// and is always valid.
func ValidateTarget(target string) error {
	if !IsGlobTarget(target) {
		return nil
	}
	if !doublestar.ValidatePattern(target) {
		return ex.Newf("target %q is not a valid glob pattern", target)
	}
	return nil
}

// MatchGlobTarget reports whether importPath satisfies the glob target pattern.
// Import-path segments are delimited by "/": "*" matches within a single
// segment, "**" matches zero or more whole segments, and "?", "[...]", and
// "{...}" follow bmatcuk/doublestar semantics
// (https://github.com/bmatcuk/doublestar#patterns).
//
// Examples:
//
//	example.com/svc/*    matches example.com/svc/users, not example.com/svc or
//	                     example.com/svc/users/v2
//	example.com/svc/**   matches example.com/svc and every descendant package
func MatchGlobTarget(pattern, importPath string) bool {
	// The pattern is validated at load time (ValidateTarget), so Match should
	// not error here; treat any error as a non-match.
	ok, err := doublestar.Match(pattern, importPath)
	return err == nil && ok
}
