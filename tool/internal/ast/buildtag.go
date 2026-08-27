// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"go/build/constraint"
	"strings"
)

// StripBuildIgnore removes genuine "//go:build ignore" constraint lines from
// content, line by line. It leaves every other occurrence of that text —
// inside a string literal, inside comment prose, anywhere that isn't itself a
// build-constraint line — untouched. See #1069: a whole-file substring replace
// corrupted both of those.
func StripBuildIgnore(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if constraint.IsGoBuild(line) {
			lines[i] = ""
		}
	}
	return strings.Join(lines, "\n")
}
