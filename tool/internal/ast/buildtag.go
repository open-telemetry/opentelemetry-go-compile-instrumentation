// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import "strings"

// StripBuildIgnore removes //go:build ignore constraints from Go source so the
// file can be parsed and injected into a target package at compile time.
func StripBuildIgnore(content string) string {
	return strings.ReplaceAll(content, "//go:build ignore", "")
}
