// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "unsafe"

func CallSizeof() {
	x := 42
	size := unsafe.Sizeof(x)
	_ = size
}

func main() {
	y := "hello"
	_ = unsafe.Sizeof(y)
}
