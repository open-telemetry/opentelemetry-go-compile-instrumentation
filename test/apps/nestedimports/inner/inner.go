// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package inner is imported by late, so the nested build that resolves late
// for dep compiles it. Its rule adds an import of leaf, a package inner does
// not import, so that compile resolves leaf with a nested build of its own.
package inner

import "strings"

// Work is wrapped by the inner rule: the injected code calls into leaf.
func Work() string {
	return strings.ToLower("FROM INNER")
}
