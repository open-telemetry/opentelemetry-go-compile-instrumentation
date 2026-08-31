// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"strings"

	"go.opentelemetry.io/otelc/tool/ex"
)

// ValidateExcludeTargets rejects malformed exclude_targets entries at load time.
func ValidateExcludeTargets(excludes []string) error {
	for i, pattern := range excludes {
		if strings.TrimSpace(pattern) == "" {
			return ex.Newf("exclude_targets[%d] is empty", i)
		}
		if err := ValidateTarget(pattern); err != nil {
			return ex.Wrapf(err, "exclude_targets[%d]", i)
		}
	}
	return nil
}

// MatchesExcludeTargets reports whether importPath matches any exclude_targets
// entry. Exact paths and glob patterns use the same semantics as target.
func MatchesExcludeTargets(importPath string, excludes []string) bool {
	for _, pattern := range excludes {
		if pattern == importPath {
			return true
		}
		if IsGlobTarget(pattern) && MatchGlobTarget(pattern, importPath) {
			return true
		}
	}
	return false
}
