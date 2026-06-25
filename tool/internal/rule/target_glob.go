// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"path"
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
)

// Glob metacharacters recognised in a rule target. A target containing any of
// these is treated as a glob pattern; otherwise it is an exact import path that
// keeps the fast map-lookup matching path.
const globMeta = "*?["

// multiSegment is the wildcard segment that matches zero or more import-path
// segments. It must appear as a whole "/"-delimited segment; a "**" fragment
// fused with other characters (e.g. "foo**") is rejected at load time.
const multiSegment = "**"

// IsGlobTarget reports whether target uses glob syntax and therefore must be
// matched against every dependency import path rather than looked up by exact
// key. An empty target is not a glob.
func IsGlobTarget(target string) bool {
	return strings.ContainsAny(target, globMeta)
}

// ValidateTarget rejects ambiguous or malformed glob targets at load time so
// that a bad rule fails loudly during parsing rather than silently matching
// nothing during the setup phase.
//
// Rules enforced:
//   - "**" is only valid as a complete segment; a fused fragment such as
//     "foo**" or "**bar" is ambiguous and rejected.
//   - every other segment must be a syntactically valid path.Match pattern;
//     this catches a malformed pattern such as an unclosed "[". Note that
//     path.Match does not treat a reversed range such as "[z-a]" as an error:
//     it returns no error and the segment simply never matches, consistent
//     with stdlib glob behaviour.
//
// An empty target is rejected upstream by the rule loader (parseRuleFromYaml)
// before it reaches ValidateTarget; here it is treated as a (trivially valid)
// exact path.
func ValidateTarget(target string) error {
	if !IsGlobTarget(target) {
		return nil
	}
	for _, seg := range strings.Split(target, "/") {
		if seg == multiSegment {
			continue
		}
		if strings.Contains(seg, multiSegment) {
			return ex.Newf("target %q is invalid: %q is only allowed as a whole path segment", target, multiSegment)
		}
		// path.Match validates the pattern syntax (bracket expressions etc.)
		// independently of the input string it is matched against.
		if _, err := path.Match(seg, ""); err != nil {
			return ex.Wrapf(err, "target %q has an invalid glob segment %q", target, seg)
		}
	}
	return nil
}

// MatchGlobTarget reports whether importPath satisfies the glob target pattern.
// Segments are delimited by "/"; within a single segment "*" matches any run of
// non-"/" characters (delegating to path.Match, so "?" and "[...]" work too),
// while the dedicated segment "**" matches zero or more whole segments.
//
// Examples:
//
//	example.com/svc/*    matches example.com/svc/users, not example.com/svc or
//	                     example.com/svc/users/v2
//	example.com/svc/**   matches example.com/svc and every descendant package
func MatchGlobTarget(pattern, importPath string) bool {
	return matchSegments(strings.Split(pattern, "/"), strings.Split(importPath, "/"))
}

// matchSegments reports whether pattern segments pat match path segments segs.
// A "**" segment matches zero or more whole segments; every other segment is a
// single-segment path.Match pattern.
//
// It uses the classic iterative wildcard algorithm with a single backtrack
// point: the most recent "**" remembers where it started absorbing, and on a
// later mismatch it absorbs one more segment. That bounds the work at
// O(len(pat) * len(segs)) even for patterns with several "**" segments, so a
// pathological target such as "**/**/**" cannot trigger exponential recursion.
func matchSegments(pat, segs []string) bool {
	pi, si := 0, 0
	starPat, starSeg := -1, -1
	for si < len(segs) {
		switch {
		case pi < len(pat) && pat[pi] == multiSegment:
			// Open a "**": record its position and the first segment it may
			// absorb, then try matching the rest with it absorbing nothing.
			starPat, starSeg = pi, si
			pi++
		case pi < len(pat) && segmentMatches(pat[pi], segs[si]):
			pi++
			si++
		case starPat >= 0:
			// Mismatch under an open "**": let it absorb one more segment.
			pi = starPat + 1
			starSeg++
			si = starSeg
		default:
			return false
		}
	}
	// Any leftover pattern must be "**" segments only, each matching zero
	// remaining path segments.
	for pi < len(pat) && pat[pi] == multiSegment {
		pi++
	}
	return pi == len(pat)
}

// segmentMatches reports whether a single non-"**" pattern segment matches one
// path segment. ValidateTarget rejects malformed patterns at load time, so
// path.Match cannot return ErrBadPattern here; a stray error is treated as no
// match, consistent with stdlib glob.
func segmentMatches(pat, seg string) bool {
	ok, err := path.Match(pat, seg)
	return err == nil && ok
}
