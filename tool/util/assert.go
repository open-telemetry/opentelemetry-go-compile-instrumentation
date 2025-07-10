// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

func Assert(condition bool, message string) {
	if !condition {
		panic("Assertion failed: " + message)
	}
}

func ShouldNotReachHere() {
	panic("should not reach here")
}
