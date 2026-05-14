// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package helper

func Wrapper(size uintptr) uintptr {
	println("Wrapped!")
	return size
}
