// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import "slices"

// normalizeModuleDirs returns a cloned, sorted, and deduplicated copy of dirs.
// If dirs is empty, it returns an empty non-nil slice. The caller's slice is never mutated.
func normalizeModuleDirs(dirs []string) []string {
	if len(dirs) == 0 {
		return []string{}
	}
	res := slices.Clone(dirs)
	slices.Sort(res)
	return slices.Compact(res)
}
