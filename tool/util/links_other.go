// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !unix && !windows

package util

// hasOneLink returns false: on this system, otelc cannot tell the number of
// hard links of a file.
func hasOneLink(string) bool {
	return false
}
