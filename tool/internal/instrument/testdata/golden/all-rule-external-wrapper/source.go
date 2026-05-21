// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "unsafe"

type T struct{}

var GlobalVar interface{} = "original"

func RawFunc() {
	println("raw func body")
}

//otelc:external-log
func DirectiveFunc() {
	println("directive func body")
}

func CallSizeof() {
	x := 42
	size := unsafe.Sizeof(x)
	_ = size
}

func main() {
	y := "hello"
	_ = unsafe.Sizeof(y)
}
