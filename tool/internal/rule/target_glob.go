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

// matchSegments recursively matches pattern segments pat against path segments
// segs. A "**" segment branches: it consumes 0..len(segs) segments, trying each
// suffix until one matches.
func matchSegments(pat, segs []string) bool {
	for {
		if len(pat) == 0 {
			return len(segs) == 0
		}
		if pat[0] == multiSegment {
			pat = pat[1:]
			for i := 0; i <= len(segs); i++ {
				if matchSegments(pat, segs[i:]) {
					return true
				}
			}
			return false
		}
		if len(segs) == 0 {
			return false
		}
		// A malformed pattern is impossible here: ValidateTarget rejects bad
		// segments at load time, so path.Match cannot return ErrBadPattern.
		ok, err := path.Match(pat[0], segs[0])
		if err != nil || !ok {
			return false
		}
		pat = pat[1:]
		segs = segs[1:]
	}
}
