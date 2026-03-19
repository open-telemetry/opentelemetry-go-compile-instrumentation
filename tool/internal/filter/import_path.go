// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package filter

import (
	stdpath "path"
	"strings"
)

// Compile-time check that ImportPathFilter implements Filter.
var _ Filter = (*ImportPathFilter)(nil)

// ImportPathFilter matches source files whose package import path satisfies
// the glob pattern. Segments are delimited by "/"; "*" matches any single
// segment, "**" matches zero or more segments.
//
// Examples:
//
//	github.com/foo/bar       exact match
//	github.com/foo/*         matches any direct child package of github.com/foo
//	github.com/foo/**        matches github.com/foo and all descendants
type ImportPathFilter struct {
	Pattern string
}

// Match reports whether ctx.ImportPath matches the glob pattern.
func (f *ImportPathFilter) Match(ctx *MatchContext) bool {
	return matchGlob(f.Pattern, ctx.ImportPath)
}

// matchGlob reports whether importPath matches pattern using "/" as the
// segment delimiter. Within a single segment, "*" matches any sequence of
// characters (but not "/"). The special segment "**" matches zero or more
// segments.
func matchGlob(pattern, importPath string) bool {
	patSegs := strings.Split(pattern, "/")
	pathSegs := strings.Split(importPath, "/")
	return matchSegments(patSegs, pathSegs)
}

// matchSegments recursively matches pat against segs.
func matchSegments(pat, segs []string) bool {
	for {
		if len(pat) == 0 {
			return len(segs) == 0
		}
		if pat[0] == "**" {
			pat = pat[1:]
			// "**" consumes 0..len(segs) path segments; try each suffix.
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
		// Single-segment match: supports *, ?, [...] via path.Match.
		ok, err := stdpath.Match(pat[0], segs[0])
		if err != nil || !ok {
			return false
		}
		pat = pat[1:]
		segs = segs[1:]
	}
}
